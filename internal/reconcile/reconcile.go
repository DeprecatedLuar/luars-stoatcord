// Package reconcile implements the one-time startup structure reconcile
// (implementation-plan.md Phase 4.5): binding pre-existing Discord and Stoat
// entities by identity before the engine's live-sync path ever runs. Without
// this, pre-existing Stoat structure has no mapping row, so the first
// category resend after Phase 4's live-sync starts wipes it (see
// implementation-plan.md Phase 4.5 context).
//
// Bind only ever performs identity matching and mapping-row writes; it never
// compares or writes entity attributes (spec 2 guardrail: comparison is
// always through canonical). Pushing Discord's full state onto a
// newly-bound entity is the converge pass's job, driven by the caller
// through the existing op builders (internal/discord) after Bind returns --
// this package stays free of discordgo and the engine's Op type.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/stoat"
)

// emptyCanonicalState is written for a freshly-bound entity's mapping row.
// "{}" never matches a real entity's canonical JSON, so the converge pass's
// Diff always reports "not equal" and pushes Discord's full state onto the
// adopted entity -- Bind never assumes an adopted Stoat entity's attributes
// already match Discord's.
const emptyCanonicalState = "{}"

// StoatReader is the subset of *internal/stoat.Client's read methods this
// package needs.
type StoatReader interface {
	FetchServer(ctx context.Context, serverID string) (stoat.ServerInfo, error)
	FetchChannel(ctx context.Context, channelID string) (stoat.ChannelInfo, error)
	FetchSelfRoleIDs(ctx context.Context, serverID string) ([]string, error)
}

// StoatWriter is the subset of *internal/stoat.Client's delete methods
// bindEntities needs to reap a foreign entity. Categories have no delete
// path here: a Stoat category is a slot in the server's category list
// (stoat.Client.SetCategories rewrites the whole list), not a
// deletable-by-id entity, so foreign categories always stay dry-run
// ("would delete") regardless of Params.DryRun until that's built.
type StoatWriter interface {
	DeleteChannel(ctx context.Context, channelID string) error
	DeleteRole(ctx context.Context, serverID, roleID string) error
}

// Params bundles Bind's dependencies. Categories/Channels/Roles are
// Discord's current structure, already translated to canonical (spec 2
// guardrail) by the caller.
type Params struct {
	ServerID string
	// GuildID is the Discord guild id. Only used by ReconcileLive, to
	// translate Stoat's "default" permission-overwrite sentinel back to
	// Discord's @everyone role -- whose id always equals the guild id (see
	// internal/discord/gateway.go's resolveOverwriteRoleIDs, which does the
	// same translation in the opposite direction).
	GuildID    string
	Categories []canonical.Category
	Channels   []canonical.Channel
	Roles      []canonical.Role
	Mappings   engine.MappingStore
	Reader     StoatReader
	Writer     StoatWriter
	// DryRun mirrors the engine's global dry-run gate (config.DryRun): when
	// true, a foreign entity is only logged ("would delete"); when false,
	// bindEntities actually deletes it (channels/roles only -- see
	// StoatWriter).
	DryRun bool
	Logger *slog.Logger
}

// identity is the name(+kind)-matching slice of an entity, common to both
// sides of the bind pass regardless of entity type.
type identity struct {
	id   string
	name string
	kind string // channel type discriminator; empty for categories/roles
}

// Bind adopts pre-existing Discord<->Stoat entities by an unambiguous
// unique name(+type) match, writing an active mapping row for each (spec:
// "Bind only on an unambiguous unique match ... Ambiguous -> log WARN and
// skip, never guess"). Unmatched Stoat entities are foreign: deleted when
// p.DryRun is false (channels/roles only -- categories always log "would
// delete", see StoatWriter), logged "would delete" otherwise. A second run
// against an already-bound state is a no-op: every Discord entity already
// has an active mapping, so nothing is unmapped left to bind.
func Bind(ctx context.Context, p Params) error {
	server, err := p.Reader.FetchServer(ctx, p.ServerID)
	if err != nil {
		return fmt.Errorf("reconcile: fetch server %s: %w", p.ServerID, err)
	}

	discordCategories := make([]identity, len(p.Categories))
	for i, c := range p.Categories {
		discordCategories[i] = identity{id: c.ID, name: c.Name}
	}
	stoatCategories := make([]identity, len(server.Categories))
	for i, c := range server.Categories {
		stoatCategories[i] = identity{id: c.ID, name: c.Title}
	}
	if err := bindEntities(ctx, p, engine.EntityCategory, discordCategories, stoatCategories, ""); err != nil {
		return err
	}

	stoatChannels := make([]identity, 0, len(server.ChannelIDs))
	for _, id := range server.ChannelIDs {
		info, err := p.Reader.FetchChannel(ctx, id)
		if err != nil {
			// A channel the bot cannot view (e.g. a role-restricted
			// channel with no ViewChannel grant) 403s on fetch. That
			// channel simply can't be identity-matched -- exclude it from
			// this pass (never bound, never flagged foreign) rather than
			// aborting the whole reconcile over one inaccessible channel.
			p.Logger.Warn("reconcile: skipping channel, fetch failed", "stoat_channel_id", id, "error", err)
			continue
		}
		stoatChannels = append(stoatChannels, identity{id: info.ID, name: info.Name, kind: string(info.Type)})
	}
	discordChannels := make([]identity, len(p.Channels))
	for i, c := range p.Channels {
		discordChannels[i] = identity{id: c.ID, name: c.Name, kind: string(c.Type)}
	}
	if err := bindEntities(ctx, p, engine.EntityChannel, discordChannels, stoatChannels, ""); err != nil {
		return err
	}

	discordRoles := make([]identity, len(p.Roles))
	for i, r := range p.Roles {
		discordRoles[i] = identity{id: r.ID, name: r.Name}
	}
	stoatRoles := make([]identity, len(server.Roles))
	for i, r := range server.Roles {
		stoatRoles[i] = identity{id: r.ID, name: r.Name}
	}

	protectedRoleID, err := botElevationRoleID(ctx, p, server.Roles)
	if err != nil {
		// Not fatal to the whole reconcile, but must be loud: with
		// p.DryRun false, a silent miss here is exactly what would let
		// the bot's own elevation role slip through the foreign-entity
		// check and get deleted.
		p.Logger.Error("reconcile: could not determine bot's elevation role, it will not be exempted from foreign-entity reap", "error", err)
	}

	return bindEntities(ctx, p, engine.EntityRole, discordRoles, stoatRoles, protectedRoleID)
}

// botElevationRoleID returns the stoat id of the bot's own highest-ranked
// role (lowest Rank value -- Stoat ranks ascend from 0 at the top, ground
// truth crates/core/permissions/src/impl.rs's get_ranking/rank check) among
// the roles the bot's Stoat member currently wears. That role must never be
// treated as a foreign, reapable entity: losing it strips the bot's
// permission elevation and it can no longer write role/permission changes
// at all (see implementation-plan.md's live-testing findings).
func botElevationRoleID(ctx context.Context, p Params, liveRoles []stoat.RoleInfo) (string, error) {
	selfRoleIDs, err := p.Reader.FetchSelfRoleIDs(ctx, p.ServerID)
	if err != nil {
		return "", fmt.Errorf("fetch self role ids: %w", err)
	}
	self := make(map[string]bool, len(selfRoleIDs))
	for _, id := range selfRoleIDs {
		self[id] = true
	}

	best := ""
	bestRank := 0
	for _, r := range liveRoles {
		if !self[r.ID] {
			continue
		}
		if best == "" || r.Rank < bestRank {
			best = r.ID
			bestRank = r.Rank
		}
	}
	return best, nil
}

// bindEntities runs the bind pass for one entity type: Discord entities with
// no active mapping are matched against Stoat entities not already claimed
// by some other active mapping, by exact name(+kind) equality. Unclaimed
// Stoat entities left over at the end are foreign (logged, dry-run).
//
// An already-active mapping whose Stoat id is no longer present in
// stoatItems is a dead mapping (the mapped entity vanished from Stoat
// without our knowledge) -- treated the same as no mapping at all, so the
// same pass's name-matching below can re-adopt a live entity of the same
// name instead of leaving it permanently unclaimed/"foreign" and the dead
// mapping permanently stale. ReconcileLive deliberately does not correct
// this case (a live-fetch miss there is logged and left untouched) --
// detecting and clearing a dead mapping is identity work, squarely Bind's
// job, not attribute-drift verification's.
// protected, when non-empty, is a stoat id that must never be flagged as a
// foreign entity regardless of claim state (the bot's own elevation role;
// see botElevationRoleID). Empty for every entity type but role.
func bindEntities(ctx context.Context, p Params, entityType engine.EntityType, discordItems, stoatItems []identity, protected string) error {
	liveStoatIDs := make(map[string]bool, len(stoatItems))
	for _, s := range stoatItems {
		liveStoatIDs[s.id] = true
	}

	claimed := make(map[string]bool, len(stoatItems))
	var unmapped []identity

	for _, d := range discordItems {
		m, err := p.Mappings.Get(string(entityType), d.id)
		if err != nil {
			return fmt.Errorf("reconcile: get mapping %s %s: %w", entityType, d.id, err)
		}
		if m.Found && m.Status == engine.StatusActive {
			if liveStoatIDs[m.StoatID] {
				claimed[m.StoatID] = true
				continue
			}
			p.Logger.Warn("reconcile: dead mapping detected, clearing for re-bind", "entity_type", entityType, "discord_id", d.id, "stale_stoat_id", m.StoatID)
			if err := p.Mappings.Remove(string(entityType), d.id); err != nil {
				return fmt.Errorf("reconcile: clear dead mapping %s %s: %w", entityType, d.id, err)
			}
		}
		unmapped = append(unmapped, d)
	}

	available := make([]identity, 0, len(stoatItems))
	for _, s := range stoatItems {
		if !claimed[s.id] {
			available = append(available, s)
		}
	}

	for _, d := range unmapped {
		matchIdx := -1
		ambiguous := false
		for i, s := range available {
			if s.name != d.name || s.kind != d.kind {
				continue
			}
			if matchIdx != -1 {
				ambiguous = true
				break
			}
			matchIdx = i
		}

		if ambiguous {
			p.Logger.Warn("reconcile: ambiguous name match, skipping bind", "entity_type", entityType, "discord_id", d.id, "name", d.name)
			continue
		}
		if matchIdx == -1 {
			// No Stoat match -- the converge pass creates it fresh.
			continue
		}

		matched := available[matchIdx]
		if err := p.Mappings.WritePending(string(entityType), d.id, emptyCanonicalState); err != nil {
			return fmt.Errorf("reconcile: bind %s %s: %w", entityType, d.id, err)
		}
		if err := p.Mappings.Confirm(string(entityType), d.id, matched.id); err != nil {
			return fmt.Errorf("reconcile: bind %s %s: %w", entityType, d.id, err)
		}
		available = append(available[:matchIdx], available[matchIdx+1:]...)
	}

	for _, s := range available {
		if s.id == protected {
			p.Logger.Info("reconcile: bot's own elevation role, exempt from foreign-entity reap", "entity_type", entityType, "stoat_id", s.id, "name", s.name)
			continue
		}
		// Categories have no delete-by-id path (StoatWriter doc comment) --
		// always dry-run regardless of p.DryRun.
		if p.DryRun || entityType == engine.EntityCategory {
			p.Logger.Warn("reconcile: foreign entity, would delete (dry run)", "entity_type", entityType, "stoat_id", s.id, "name", s.name)
			continue
		}
		var err error
		switch entityType {
		case engine.EntityChannel:
			err = p.Writer.DeleteChannel(ctx, s.id)
		case engine.EntityRole:
			err = p.Writer.DeleteRole(ctx, p.ServerID, s.id)
		}
		if err != nil {
			p.Logger.Error("reconcile: foreign entity, delete failed", "entity_type", entityType, "stoat_id", s.id, "name", s.name, "error", err)
			continue
		}
		p.Logger.Warn("reconcile: foreign entity, deleted", "entity_type", entityType, "stoat_id", s.id, "name", s.name)
	}
	return nil
}
