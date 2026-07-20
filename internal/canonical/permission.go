// Package canonical defines the neutral entity, operation and permission
// model that Discord and Stoat translators both target (spec 2). It imports
// neither discordgo nor the Stoat/Revolt client library.
package canonical

import "log/slog"

// Permission is the canonical permission vocabulary (spec 4): the union of
// Discord's permission set, each entry either mapped to a Stoat equivalent
// or explicitly marked as a drop.
type Permission string

const (
	PermViewChannel     Permission = "VIEW_CHANNEL"
	PermReadHistory     Permission = "READ_HISTORY"
	PermSendMessages    Permission = "SEND_MESSAGES"
	PermManageMessages  Permission = "MANAGE_MESSAGES"
	PermEmbedLinks      Permission = "EMBED_LINKS"
	PermAttachFiles     Permission = "ATTACH_FILES"
	PermAddReactions    Permission = "ADD_REACTIONS"
	PermManageChannel   Permission = "MANAGE_CHANNEL"
	PermManageServer    Permission = "MANAGE_SERVER"
	PermManageRoles     Permission = "MANAGE_ROLES"
	PermCreateInvite    Permission = "CREATE_INVITE"
	PermConnect         Permission = "CONNECT"
	PermSpeak           Permission = "SPEAK"
	PermVideo           Permission = "VIDEO"
	PermMuteMembers     Permission = "MUTE_MEMBERS"
	PermDeafenMembers   Permission = "DEAFEN_MEMBERS"
	PermMoveMembers     Permission = "MOVE_MEMBERS"
	PermManageWebhooks  Permission = "MANAGE_WEBHOOKS"
	PermManageEmoji     Permission = "MANAGE_EMOJI"
	PermManageNicks     Permission = "MANAGE_NICKNAMES"
	PermKickMembers     Permission = "KICK_MEMBERS"
	PermBanMembers      Permission = "BAN_MEMBERS"
	PermTimeoutMembers  Permission = "TIMEOUT_MEMBERS"
	PermMentionEveryone Permission = "MENTION_EVERYONE"

	// Drops (spec 4): no reachable Stoat equivalent, logged whenever
	// encountered. PermViewAuditLog's bit (ViewAuditLogs, 1<<40) exists in
	// Stoat's enum, but Stoat's own web client exposes no UI toggle for it
	// anywhere -- the only way to grant it to any role is a raw API call by
	// the server owner, bypassing normal administration entirely. Treated as
	// unreachable in practice, same as a true no-equivalent drop.
	PermViewAuditLog    Permission = "VIEW_AUDIT_LOG"
	PermUseAppCommands  Permission = "USE_APP_COMMANDS"
	PermPrioritySpeaker Permission = "PRIORITY_SPEAKER"
	PermSendTTS         Permission = "SEND_TTS"
)

// stoatBits maps each canonical permission with a Stoat equivalent to its
// bit value. Values are sourced from the live Stoat ChannelPermission enum
// (crates/core/permissions/src/models/channel.rs, stoatchat/stoatchat),
// reconciled in Phase 0 -- never hand-guessed (see implementation-plan.md's
// "how to re-verify" note).
var stoatBits = map[Permission]uint64{
	PermManageChannel:   1 << 0,
	PermManageServer:    1 << 1,
	PermManageRoles:     1 << 2,
	PermManageEmoji:     1 << 4,
	PermKickMembers:     1 << 6,
	PermBanMembers:      1 << 7,
	PermTimeoutMembers:  1 << 8,
	PermManageNicks:     1 << 11,
	PermViewChannel:     1 << 20,
	PermReadHistory:     1 << 21,
	PermSendMessages:    1 << 22,
	PermManageMessages:  1 << 23,
	PermManageWebhooks:  1 << 24,
	PermCreateInvite:    1 << 25,
	PermEmbedLinks:      1 << 26,
	PermAttachFiles:     1 << 27,
	PermAddReactions:    1 << 29,
	PermConnect:         1 << 30,
	PermSpeak:           1 << 31,
	PermVideo:           1 << 32,
	PermMuteMembers:     1 << 33,
	PermDeafenMembers:   1 << 34,
	PermMoveMembers:     1 << 35,
	PermMentionEveryone: 1 << 37,
}

// StoatBits aggregates perms into a single Stoat permission bitmask. Any
// permission with no Stoat equivalent (spec 4: explicit drops, and by the
// same rule anything unrecognized) is skipped and logged at WARN -- never a
// silent loss.
func StoatBits(perms []Permission, logger *slog.Logger) uint64 {
	var bits uint64
	for _, p := range perms {
		bit, ok := stoatBits[p]
		if !ok {
			logger.Warn("canonical: permission dropped, no Stoat equivalent", "permission", p)
			continue
		}
		bits |= bit
	}
	return bits
}

// Overwrite is the canonical tri-state permission overwrite (spec 3): a role
// may be explicitly allowed, explicitly denied, or neither (inherit).
type Overwrite struct {
	Allow []Permission
	Deny  []Permission
}

// StoatOverwrite is Stoat's wire-shaped tri-state overwrite,
// {"permissions":{"allow":N,"deny":N}} (implementation-plan.md gap 2).
type StoatOverwrite struct {
	Allow uint64
	Deny  uint64
}

// ToStoat translates a canonical overwrite to Stoat's wire shape, logging
// any dropped permissions encountered in either the allow or deny set.
func (o Overwrite) ToStoat(logger *slog.Logger) StoatOverwrite {
	return StoatOverwrite{
		Allow: StoatBits(o.Allow, logger),
		Deny:  StoatBits(o.Deny, logger),
	}
}
