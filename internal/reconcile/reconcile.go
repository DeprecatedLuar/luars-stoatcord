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

// foreignDryRun gates destructive action on a foreign Stoat entity (spec:
// "Destructive actions are dry-run during testing"). Bind never deletes
// regardless -- real deletion is a later phase's scope -- this constant only
// documents the gate so a future phase flips one place, not scattered ones.
const foreignDryRun = true

// StoatReader is the subset of *internal/stoat.Client's read methods this
// package needs.
type StoatReader interface {
	FetchServer(ctx context.Context, serverID string) (stoat.ServerInfo, error)
	FetchChannel(ctx context.Context, channelID string) (stoat.ChannelInfo, error)
}

// Params bundles Bind's dependencies. Categories/Channels/Roles are
// Discord's current structure, already translated to canonical (spec 2
// guardrail) by the caller.
type Params struct {
	ServerID   string
	Categories []canonical.Category
	Channels   []canonical.Channel
	Roles      []canonical.Role
	Mappings   engine.MappingStore
	Reader     StoatReader
	Logger     *slog.Logger
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
// skip, never guess"). Unmatched Stoat entities are logged as foreign
// ("would delete", dry-run only). A second run against an already-bound
// state is a no-op: every Discord entity already has an active mapping, so
// nothing is unmapped left to bind.
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
	if err := bindEntities(p, engine.EntityCategory, discordCategories, stoatCategories); err != nil {
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
	if err := bindEntities(p, engine.EntityChannel, discordChannels, stoatChannels); err != nil {
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
	return bindEntities(p, engine.EntityRole, discordRoles, stoatRoles)
}

// bindEntities runs the bind pass for one entity type: Discord entities with
// no active mapping are matched against Stoat entities not already claimed
// by some other active mapping, by exact name(+kind) equality. Unclaimed
// Stoat entities left over at the end are foreign (logged, dry-run).
func bindEntities(p Params, entityType engine.EntityType, discordItems, stoatItems []identity) error {
	claimed := make(map[string]bool, len(stoatItems))
	var unmapped []identity

	for _, d := range discordItems {
		m, err := p.Mappings.Get(string(entityType), d.id)
		if err != nil {
			return fmt.Errorf("reconcile: get mapping %s %s: %w", entityType, d.id, err)
		}
		if m.Found && m.Status == engine.StatusActive {
			claimed[m.StoatID] = true
			continue
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
		if foreignDryRun {
			p.Logger.Warn("reconcile: foreign entity, would delete (dry run)", "entity_type", entityType, "stoat_id", s.id, "name", s.name)
		}
	}
	return nil
}
