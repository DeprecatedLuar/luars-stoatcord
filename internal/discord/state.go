package discord

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
)

// StructureFromState reads the mirrored guild's current categories,
// channels, and roles from discordgo's state cache, already translated to
// canonical (spec 2 guardrail: comparison/matching is always through
// canonical, never a platform type reaching outside its own adapter).
func StructureFromState(s *discordgo.Session, guildID string, logger *slog.Logger) (categories []canonical.Category, channels []canonical.Channel, roles []canonical.Role) {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		logger.Error("discord: failed to read guild from state cache", "guild_id", guildID, "error", err)
		return nil, nil, nil
	}

	categories = categoriesFromState(s, guildID, logger)
	for _, ch := range guild.Channels {
		if cc, ok := ChannelFromDiscord(ch, logger); ok {
			channels = append(channels, cc)
		}
	}
	for _, r := range guild.Roles {
		roles = append(roles, RoleFromDiscord(r, logger))
	}
	return categories, channels, roles
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
	positions := map[string]int{}
	members := map[string][]*discordgo.Channel{}
	for _, ch := range guild.Channels {
		if ch.Type == discordgo.ChannelTypeGuildCategory {
			names[ch.ID] = ch.Name
			positions[ch.ID] = ch.Position
		}
	}
	for _, ch := range guild.Channels {
		if ch.ParentID == "" {
			continue
		}
		if _, isCategory := names[ch.ParentID]; !isCategory {
			continue
		}
		if _, ok := ChannelTypeFromDiscord(ch.Type); !ok {
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
	// Sort by the category channel's own Discord Position, not by ID, so
	// Stoat's category order matches the Discord sidebar order.
	slices.SortFunc(cats, func(a, b canonical.Category) int {
		return positions[a.ID] - positions[b.ID]
	})
	// Position is each category's index in this sort, not the raw Discord
	// integer -- Discord's Position values can have gaps or ties, but the
	// canonical order needs to be a clean, comparable 0..n-1 sequence so
	// BuildCategoryOp's Diff can detect a reorder.
	for i := range cats {
		cats[i].Position = i
	}
	return cats
}

// WaitForGuildReady blocks until guildID's GUILD_CREATE event has populated
// session's state cache, or ctx/timeout expires first. session.Open()
// returns on READY, before per-guild data arrives -- reading state before
// this returns yields empty categories/channels/roles.
func WaitForGuildReady(ctx context.Context, session *discordgo.Session, guildID string, timeout time.Duration) error {
	if _, err := session.State.Guild(guildID); err == nil {
		return nil
	}

	ready := make(chan struct{})
	remove := session.AddHandler(func(s *discordgo.Session, e *discordgo.GuildCreate) {
		if e.ID != guildID {
			return
		}
		select {
		case <-ready:
		default:
			close(ready)
		}
	})
	defer remove()

	// Re-check after registering: GUILD_CREATE may have fired between the
	// first check above and AddHandler.
	if _, err := session.State.Guild(guildID); err == nil {
		return nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("timed out after %s waiting for guild %s GUILD_CREATE", timeout, guildID)
	}
}
