package discord

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
)

// channelTypeMap flattens Discord's channel types onto the canonical two
// (spec 6): announcement/stage/forum lose type-specific semantics but keep
// messages. Categories and threads are not channel types here -- categories
// are server-level (spec 6 note), threads are deferred post-v1 (spec 9).
var channelTypeMap = map[discordgo.ChannelType]canonical.ChannelType{
	discordgo.ChannelTypeGuildText:       canonical.ChannelTypeText,
	discordgo.ChannelTypeGuildNews:       canonical.ChannelTypeText,
	discordgo.ChannelTypeGuildForum:      canonical.ChannelTypeText,
	discordgo.ChannelTypeGuildVoice:      canonical.ChannelTypeVoice,
	discordgo.ChannelTypeGuildStageVoice: canonical.ChannelTypeVoice,
}

// ChannelTypeFromDiscord flattens a Discord channel type to its canonical
// equivalent. ok is false for types this mirror does not model as a channel
// (category, threads, DMs).
func ChannelTypeFromDiscord(t discordgo.ChannelType) (canonical.ChannelType, bool) {
	ct, ok := channelTypeMap[t]
	return ct, ok
}

// OverwriteFromDiscord translates a single Discord permission overwrite to
// its canonical role-keyed form. Member-level overwrites have no Stoat
// target entity (spec 7) -- dropped and logged, ok is false.
func OverwriteFromDiscord(ow *discordgo.PermissionOverwrite, logger *slog.Logger) (roleID string, overwrite canonical.Overwrite, ok bool) {
	if ow.Type == discordgo.PermissionOverwriteTypeMember {
		logger.Warn("discord: member-level permission overwrite dropped, no Stoat target entity", "member_id", ow.ID)
		return "", canonical.Overwrite{}, false
	}

	return ow.ID, canonical.Overwrite{
		Allow: PermissionsFromBits(ow.Allow, logger),
		Deny:  PermissionsFromBits(ow.Deny, logger),
	}, true
}

// ChannelFromDiscord translates a Discord channel to canonical. ok is false
// for channel types this mirror does not model (see ChannelTypeFromDiscord).
func ChannelFromDiscord(ch *discordgo.Channel, logger *slog.Logger) (canonical.Channel, bool) {
	ct, ok := ChannelTypeFromDiscord(ch.Type)
	if !ok {
		return canonical.Channel{}, false
	}

	overwrites := make(map[string]canonical.Overwrite, len(ch.PermissionOverwrites))
	for _, ow := range ch.PermissionOverwrites {
		roleID, cow, ok := OverwriteFromDiscord(ow, logger)
		if !ok {
			continue
		}
		overwrites[roleID] = cow
	}

	return canonical.Channel{
		ID:         ch.ID,
		Name:       ch.Name,
		Type:       ct,
		CategoryID: ch.ParentID,
		Position:   ch.Position,
		Overwrites: overwrites,
	}, true
}
