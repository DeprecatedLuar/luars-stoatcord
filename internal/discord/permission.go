// Package discord is the discordgo adapter (spec target layout): events and
// REST backfill translated to canonical. Confined here per the architecture
// guardrail -- internal/canonical never imports discordgo.
package discord

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
)

// discordPermissionBits is the width of Discord's permission bitmask.
const discordPermissionBits = 64

// bitsToCanonical maps each Discord permission bit with a canonical
// equivalent (spec 4) to that Permission. Values are discordgo's own
// exported constants, never hand-copied.
var bitsToCanonical = map[int64]canonical.Permission{
	discordgo.PermissionViewChannel:            canonical.PermViewChannel,
	discordgo.PermissionReadMessageHistory:     canonical.PermReadHistory,
	discordgo.PermissionSendMessages:           canonical.PermSendMessages,
	discordgo.PermissionManageMessages:         canonical.PermManageMessages,
	discordgo.PermissionEmbedLinks:             canonical.PermEmbedLinks,
	discordgo.PermissionAttachFiles:            canonical.PermAttachFiles,
	discordgo.PermissionAddReactions:           canonical.PermAddReactions,
	discordgo.PermissionManageChannels:         canonical.PermManageChannel,
	discordgo.PermissionManageGuild:            canonical.PermManageServer,
	discordgo.PermissionManageRoles:            canonical.PermManageRoles,
	discordgo.PermissionCreateInstantInvite:    canonical.PermCreateInvite,
	discordgo.PermissionVoiceConnect:           canonical.PermConnect,
	discordgo.PermissionVoiceSpeak:             canonical.PermSpeak,
	discordgo.PermissionVoiceStreamVideo:       canonical.PermVideo,
	discordgo.PermissionVoiceMuteMembers:       canonical.PermMuteMembers,
	discordgo.PermissionVoiceDeafenMembers:     canonical.PermDeafenMembers,
	discordgo.PermissionVoiceMoveMembers:       canonical.PermMoveMembers,
	discordgo.PermissionManageWebhooks:         canonical.PermManageWebhooks,
	discordgo.PermissionManageGuildExpressions: canonical.PermManageEmoji,
	discordgo.PermissionManageNicknames:        canonical.PermManageNicks,
	discordgo.PermissionKickMembers:            canonical.PermKickMembers,
	discordgo.PermissionBanMembers:             canonical.PermBanMembers,
	discordgo.PermissionModerateMembers:        canonical.PermTimeoutMembers,
	discordgo.PermissionMentionEveryone:        canonical.PermMentionEveryone,
}

// bitsDropped names Discord permission bits that are recognized but have no
// reachable canonical/Stoat equivalent (spec 4 explicit drops, plus
// Administrator -- out of scope, not represented in the canonical vocabulary
// at all). ViewAuditLogs has a real Stoat bit but no UI path to grant it on
// any role (see canonical.PermViewAuditLog), so it's treated as a drop too.
var bitsDropped = map[int64]string{
	discordgo.PermissionUseApplicationCommands: string(canonical.PermUseAppCommands),
	discordgo.PermissionVoicePrioritySpeaker:   string(canonical.PermPrioritySpeaker),
	discordgo.PermissionSendTTSMessages:        string(canonical.PermSendTTS),
	discordgo.PermissionAdministrator:          "ADMINISTRATOR",
	discordgo.PermissionViewAuditLogs:          string(canonical.PermViewAuditLog),
}

// PermissionsFromBits decodes a Discord permission bitmask into canonical
// permissions. Any set bit with no canonical equivalent -- named drop or
// genuinely unrecognized -- is skipped and logged at WARN, never silently
// lost (spec 4).
func PermissionsFromBits(bits int64, logger *slog.Logger) []canonical.Permission {
	var perms []canonical.Permission
	for bitPos := range discordPermissionBits {
		mask := int64(1) << uint(bitPos)
		if bits&mask == 0 {
			continue
		}
		if p, ok := bitsToCanonical[mask]; ok {
			perms = append(perms, p)
			continue
		}
		if name, ok := bitsDropped[mask]; ok {
			logger.Warn("discord: permission dropped, no Stoat equivalent", "permission", name)
			continue
		}
		logger.Warn("discord: unrecognized permission bit", "bit", bitPos)
	}
	return perms
}
