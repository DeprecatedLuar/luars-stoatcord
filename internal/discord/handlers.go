package discord

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

// Writer is the union of every entity writer this package's session-level
// helpers (RegisterHandlers, ConvergeAll) need. Individual op builders keep
// taking their own narrow interface (ChannelWriter, CategoryWriter,
// RoleWriter); this exists only because these two functions submit ops for
// every entity kind from a single caller-supplied writer.
type Writer interface {
	ChannelWriter
	CategoryWriter
	RoleWriter
}

// RegisterHandlers wires discordgo structure events to engine.Op
// submissions, scoped to guildID (the mirrored guild -- events from any
// other guild the bot happens to be in are ignored).
func RegisterHandlers(session *discordgo.Session, guildID, stoatServerID string, mappings MappingReader, writer Writer, eng *engine.Engine, logger *slog.Logger) {
	session.AddHandler(func(s *discordgo.Session, e *discordgo.ChannelCreate) {
		if e.GuildID != guildID {
			return
		}
		handleChannelChange(engine.OpCreate, e.Channel, s, guildID, stoatServerID, mappings, writer, eng, logger)
	})
	session.AddHandler(func(s *discordgo.Session, e *discordgo.ChannelUpdate) {
		if e.GuildID != guildID {
			return
		}
		handleChannelChange(engine.OpUpdate, e.Channel, s, guildID, stoatServerID, mappings, writer, eng, logger)
	})
	session.AddHandler(func(s *discordgo.Session, e *discordgo.ChannelDelete) {
		if e.GuildID != guildID {
			return
		}
		eng.Submit(BuildChannelDeleteOp(e.Channel.ID, mappings, writer))
		submitCategoryOps(s, guildID, stoatServerID, mappings, writer, eng, logger)
	})

	session.AddHandler(func(s *discordgo.Session, e *discordgo.GuildRoleCreate) {
		if e.GuildID != guildID {
			return
		}
		eng.Submit(BuildRoleOp(engine.OpCreate, e.Role, stoatServerID, mappings, writer, logger))
	})
	session.AddHandler(func(s *discordgo.Session, e *discordgo.GuildRoleUpdate) {
		if e.GuildID != guildID {
			return
		}
		eng.Submit(BuildRoleOp(engine.OpUpdate, e.Role, stoatServerID, mappings, writer, logger))
	})
	session.AddHandler(func(s *discordgo.Session, e *discordgo.GuildRoleDelete) {
		if e.GuildID != guildID {
			return
		}
		eng.Submit(BuildRoleDeleteOp(e.RoleID, stoatServerID, mappings, writer))
	})
}

// ConvergeAll submits an update op for every role, channel, and category
// currently in guildID's state cache. Used once at startup (after the
// reconcile bind pass) so bound entities get Discord's full state pushed and
// never-before-seen entities get created for real.
func ConvergeAll(session *discordgo.Session, guildID, stoatServerID string, mappings MappingReader, writer Writer, eng *engine.Engine, logger *slog.Logger) {
	guild, err := session.State.Guild(guildID)
	if err != nil {
		logger.Error("discord: failed to read guild from state cache", "guild_id", guildID, "error", err)
		return
	}

	for _, r := range guild.Roles {
		eng.Submit(BuildRoleOp(engine.OpUpdate, r, stoatServerID, mappings, writer, logger))
	}
	for _, ch := range guild.Channels {
		if op, ok := BuildChannelOp(engine.OpUpdate, ch, guildID, stoatServerID, mappings, writer, logger); ok {
			eng.Submit(op)
		}
	}
	submitCategoryOps(session, guildID, stoatServerID, mappings, writer, eng, logger)
}

// handleChannelChange submits the channel's own op, then re-derives and
// resubmits every category (a channel's category membership or position may
// have changed, and Stoat has no per-category edit -- spec 6).
func handleChannelChange(kind engine.OpKind, ch *discordgo.Channel, s *discordgo.Session, guildID, stoatServerID string, mappings MappingReader, writer Writer, eng *engine.Engine, logger *slog.Logger) {
	if ch.Type == discordgo.ChannelTypeGuildCategory {
		submitCategoryOps(s, guildID, stoatServerID, mappings, writer, eng, logger)
		return
	}
	if op, ok := BuildChannelOp(kind, ch, guildID, stoatServerID, mappings, writer, logger); ok {
		eng.Submit(op)
	}
	submitCategoryOps(s, guildID, stoatServerID, mappings, writer, eng, logger)
}

// submitCategoryOps re-derives every category from the guild's current
// (state-cache) channel layout and submits one op per category. Cheap
// enough to call on every structure change: state is already in memory, no
// REST calls happen until Apply actually runs.
func submitCategoryOps(s *discordgo.Session, guildID, stoatServerID string, mappings MappingReader, writer CategoryWriter, eng *engine.Engine, logger *slog.Logger) {
	cats := categoriesFromState(s, guildID, logger)
	snapshot := func() []canonical.Category { return cats }
	for _, cat := range cats {
		eng.Submit(BuildCategoryOp(engine.OpUpdate, cat, snapshot, stoatServerID, mappings, writer, logger))
	}
}
