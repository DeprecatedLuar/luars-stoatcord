package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/discord"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/reconcile"
	"github.com/luar/stoatcord/internal/stoat"
)

const (
	// guildReadyTimeout bounds how long startup waits for the mirrored
	// guild's own GUILD_CREATE event. discordSession.Open() returns on
	// READY, before per-guild data arrives -- reading the state cache
	// before this yields empty categories/channels/roles.
	guildReadyTimeout = 30 * time.Second

	// stoatHealthyTimeout/stoatHealthyPollInterval bound how long startup
	// waits for the Stoat gateway to report healthy before running the
	// converge pass. Converging before Stoat is healthy would just enqueue
	// every op to op_queue instead of applying (engine's health gate),
	// which is safe but pointless on a normal fresh start.
	stoatHealthyTimeout      = 30 * time.Second
	stoatHealthyPollInterval = 100 * time.Millisecond
)

// waitForGuildReady blocks until guildID's GUILD_CREATE event has populated
// discordSession's state cache, or ctx/timeout expires first.
func waitForGuildReady(ctx context.Context, session *discordgo.Session, guildID string, timeout time.Duration) error {
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

// waitForStoatHealthy polls gw until it reports healthy, or ctx/timeout
// expires first.
func waitForStoatHealthy(ctx context.Context, gw *stoat.Gateway, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if gw.Healthy() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for stoat gateway to become healthy", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(stoatHealthyPollInterval):
		}
	}
}

// discordStructureFromState reads the mirrored guild's current categories,
// channels, and roles from discordgo's state cache, already translated to
// canonical (spec 2 guardrail: comparison/matching is always through
// canonical, never a platform type reaching outside its own adapter).
func discordStructureFromState(s *discordgo.Session, guildID string, logger *slog.Logger) (categories []canonical.Category, channels []canonical.Channel, roles []canonical.Role) {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		logger.Error("discord: failed to read guild from state cache", "guild_id", guildID, "error", err)
		return nil, nil, nil
	}

	categories = categoriesFromState(s, guildID, logger)
	for _, ch := range guild.Channels {
		if cc, ok := discord.ChannelFromDiscord(ch, logger); ok {
			channels = append(channels, cc)
		}
	}
	for _, r := range guild.Roles {
		roles = append(roles, discord.RoleFromDiscord(r, logger))
	}
	return categories, channels, roles
}

// runStartupReconcile performs the Phase 4.5 startup structure reconcile:
// bind pre-existing Discord<->Stoat entities by identity (reconcile.Bind),
// then converge every current Discord entity through the engine so bound
// entities get Discord's full state pushed and never-before-seen entities
// get created for real. Must run after both the Discord guild state and the
// Stoat gateway are ready (see waitForGuildReady/waitForStoatHealthy),
// otherwise the bind pass reads stale/empty structure or the converge pass's
// ops just pile up in op_queue.
func runStartupReconcile(ctx context.Context, session *discordgo.Session, guildID, stoatServerID string, mappings discord.MappingReader, reader reconcile.StoatReader, mappingStore engine.MappingStore, writer *stoat.Client, eng *engine.Engine, logger *slog.Logger) error {
	guild, err := session.State.Guild(guildID)
	if err != nil {
		return fmt.Errorf("reconcile: read guild %s from state cache: %w", guildID, err)
	}

	categories, channels, roles := discordStructureFromState(session, guildID, logger)

	if err := reconcile.Bind(ctx, reconcile.Params{
		ServerID:   stoatServerID,
		Categories: categories,
		Channels:   channels,
		Roles:      roles,
		Mappings:   mappingStore,
		Reader:     reader,
		Logger:     logger,
	}); err != nil {
		return fmt.Errorf("reconcile: bind pass: %w", err)
	}

	for _, r := range guild.Roles {
		eng.Submit(discord.BuildRoleOp(engine.OpUpdate, r, stoatServerID, mappings, writer, logger))
	}
	for _, ch := range guild.Channels {
		if op, ok := discord.BuildChannelOp(engine.OpUpdate, ch, stoatServerID, mappings, writer, logger); ok {
			eng.Submit(op)
		}
	}
	submitCategoryOps(session, guildID, stoatServerID, mappings, writer, eng, logger)

	return nil
}
