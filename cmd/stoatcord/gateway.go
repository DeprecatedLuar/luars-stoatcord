package main

import (
	"log/slog"
	"slices"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/discord"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/stoat"
)

// registerDiscordHandlers wires discordgo structure events to engine.Op
// submissions, scoped to guildID (the mirrored guild -- events from any
// other guild the bot happens to be in are ignored).
func registerDiscordHandlers(session *discordgo.Session, guildID, stoatServerID string, mappings discord.MappingReader, writer *stoat.Client, eng *engine.Engine, logger *slog.Logger) {
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
		eng.Submit(discord.BuildChannelDeleteOp(e.Channel.ID, mappings, writer))
		submitCategoryOps(s, guildID, stoatServerID, mappings, writer, eng, logger)
	})

	session.AddHandler(func(s *discordgo.Session, e *discordgo.GuildRoleCreate) {
		if e.GuildID != guildID {
			return
		}
		eng.Submit(discord.BuildRoleOp(engine.OpCreate, e.Role, stoatServerID, mappings, writer, logger))
	})
	session.AddHandler(func(s *discordgo.Session, e *discordgo.GuildRoleUpdate) {
		if e.GuildID != guildID {
			return
		}
		eng.Submit(discord.BuildRoleOp(engine.OpUpdate, e.Role, stoatServerID, mappings, writer, logger))
	})
	session.AddHandler(func(s *discordgo.Session, e *discordgo.GuildRoleDelete) {
		if e.GuildID != guildID {
			return
		}
		eng.Submit(discord.BuildRoleDeleteOp(e.RoleID, stoatServerID, mappings, writer))
	})
}

// handleChannelChange submits the channel's own op, then re-derives and
// resubmits every category (a channel's category membership or position may
// have changed, and Stoat has no per-category edit -- spec 6).
func handleChannelChange(kind engine.OpKind, ch *discordgo.Channel, s *discordgo.Session, guildID, stoatServerID string, mappings discord.MappingReader, writer *stoat.Client, eng *engine.Engine, logger *slog.Logger) {
	if ch.Type == discordgo.ChannelTypeGuildCategory {
		submitCategoryOps(s, guildID, stoatServerID, mappings, writer, eng, logger)
		return
	}
	if op, ok := discord.BuildChannelOp(kind, ch, stoatServerID, mappings, writer, logger); ok {
		eng.Submit(op)
	}
	submitCategoryOps(s, guildID, stoatServerID, mappings, writer, eng, logger)
}

// submitCategoryOps re-derives every category from the guild's current
// (state-cache) channel layout and submits one op per category. Cheap
// enough to call on every structure change: state is already in memory, no
// REST calls happen until Apply actually runs.
func submitCategoryOps(s *discordgo.Session, guildID, stoatServerID string, mappings discord.MappingReader, writer discord.CategoryWriter, eng *engine.Engine, logger *slog.Logger) {
	cats := categoriesFromState(s, guildID, logger)
	snapshot := func() []canonical.Category { return cats }
	for _, cat := range cats {
		eng.Submit(discord.BuildCategoryOp(engine.OpUpdate, cat, snapshot, stoatServerID, mappings, writer))
	}
}

// categoriesFromState derives every category and its ordered channel
// membership from discordgo's state cache. Channel types this mirror does
// not model (spec 6: threads, DMs) are excluded from membership.
func categoriesFromState(s *discordgo.Session, guildID string, logger *slog.Logger) []canonical.Category {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		logger.Error("discord: failed to read guild from state cache", "guild_id", guildID, "error", err)
		return nil
	}

	names := map[string]string{}
	members := map[string][]*discordgo.Channel{}
	for _, ch := range guild.Channels {
		if ch.Type == discordgo.ChannelTypeGuildCategory {
			names[ch.ID] = ch.Name
		}
	}
	for _, ch := range guild.Channels {
		if ch.ParentID == "" {
			continue
		}
		if _, isCategory := names[ch.ParentID]; !isCategory {
			continue
		}
		if _, ok := discord.ChannelTypeFromDiscord(ch.Type); !ok {
			continue
		}
		members[ch.ParentID] = append(members[ch.ParentID], ch)
	}

	cats := make([]canonical.Category, 0, len(names))
	for id, name := range names {
		channels := members[id]
		slices.SortFunc(channels, func(a, b *discordgo.Channel) int { return a.Position - b.Position })
		channelIDs := make([]string, len(channels))
		for i, ch := range channels {
			channelIDs[i] = ch.ID
		}
		cats = append(cats, canonical.Category{ID: id, Name: name, ChannelIDs: channelIDs})
	}
	slices.SortFunc(cats, func(a, b canonical.Category) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return cats
}
