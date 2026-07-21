package reconcile

import (
	"context"
	"fmt"
	"reflect"

	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/stoat"
)

// ReconcileLive re-verifies every already-bound (active) category, channel,
// and role against Stoat's actual live state, not the locally cached
// canonical_state the engine's Diff normally trusts. Bind alone only
// guarantees this for entities newly adopted in this run -- an entity
// bound in a prior run, whose remote write silently drifted since, is
// never re-checked by anything else in this codebase.
//
// Runs once at startup only, between Bind and the live converge pass. A
// full live fetch-and-compare on every Discord event would blow the rate
// limiter's budget for no benefit -- the live handlers already trust their
// own last write, and that trust is exactly what this pass exists to
// verify periodically at process start, not continuously.
//
// For each entity with an active mapping: fetch its live Stoat state, diff
// it against Discord's desired canonical state (in the same Stoat-wire
// comparison space the existing op builders' Diff closures already use).
// Match -> re-stamp the mapping row with the desired canonical_state, so
// the converge pass's Diff reports equal and skips a spurious remote
// write. Mismatch -> reset canonical_state to "{}" (same trick Bind uses
// to adopt a foreign entity) so the converge pass's Diff reports
// not-equal, forcing a real corrective push through the unmodified
// op-builder Apply path -- dependency resolution, rate limiting, health
// gating, and create-vs-edit branching are all reused, never
// reimplemented here.
//
// Both branches call Confirm immediately after WritePending:
// WritePending always flips status to pending, and nothing else
// re-confirms an already-active row whose Diff later finds equal --
// skipping this would silently leave the row pending and break
// DependsOn gating for anything depending on it.
//
// A live fetch failure (entity deleted directly on Stoat, not merely
// drifted) is logged WARN and the mapping row is left untouched --
// deciding what to do about a vanished Stoat entity is out of scope for
// this pass, matching Bind's existing stance that deletion/foreign-entity
// handling belongs to a later phase.
func ReconcileLive(ctx context.Context, p Params) error {
	server, err := p.Reader.FetchServer(ctx, p.ServerID)
	if err != nil {
		return fmt.Errorf("reconcile: live fetch server %s: %w", p.ServerID, err)
	}

	stoatToDiscordRole, err := reverseMapping(p, engine.EntityRole, roleIDs(p.Roles))
	if err != nil {
		return err
	}
	stoatToDiscordChannel, err := reverseMapping(p, engine.EntityChannel, channelIDs(p.Channels))
	if err != nil {
		return err
	}

	for _, role := range p.Roles {
		if err := reconcileRoleLive(p, role, server.Roles); err != nil {
			return err
		}
	}
	for _, ch := range p.Channels {
		if err := reconcileChannelLive(ctx, p, ch, stoatToDiscordRole); err != nil {
			return err
		}
	}
	for _, cat := range p.Categories {
		if err := reconcileCategoryLive(p, cat, server.Categories, stoatToDiscordChannel); err != nil {
			return err
		}
	}
	return nil
}

// roleIDs/channelIDs extract discord ids for reverseMapping's lookup loop.
func roleIDs(roles []canonical.Role) []string {
	ids := make([]string, len(roles))
	for i, r := range roles {
		ids[i] = r.ID
	}
	return ids
}

func channelIDs(channels []canonical.Channel) []string {
	ids := make([]string, len(channels))
	for i, c := range channels {
		ids[i] = c.ID
	}
	return ids
}

// reverseMapping builds stoatID -> discordID for every discordID with an
// active mapping row, the inverse of the identity translation
// resolveOverwriteRoleIDs (internal/discord/gateway.go) does at Apply time.
func reverseMapping(p Params, entityType engine.EntityType, discordIDs []string) (map[string]string, error) {
	reverse := make(map[string]string, len(discordIDs))
	for _, discordID := range discordIDs {
		m, err := p.Mappings.Get(string(entityType), discordID)
		if err != nil {
			return nil, fmt.Errorf("reconcile: live get mapping %s %s: %w", entityType, discordID, err)
		}
		if !m.Found || m.Status != engine.StatusActive {
			continue
		}
		reverse[m.StoatID] = discordID
	}
	return reverse, nil
}

// repairMapping re-stamps an active mapping row's canonical_state so the
// converge pass's Diff reads a trustworthy value: the entity's own desired
// state on a match, or the empty-state sentinel to force a real corrective
// push on a mismatch. See ReconcileLive's doc comment for why both branches
// must re-Confirm.
func repairMapping(p Params, entityType engine.EntityType, discordID, stoatID string, matches bool, desiredJSON []byte, mismatchLogArgs ...any) error {
	state := string(desiredJSON)
	if !matches {
		state = emptyCanonicalState
		args := append([]any{"entity_type", entityType, "discord_id", discordID}, mismatchLogArgs...)
		p.Logger.Warn("reconcile: live drift detected, forcing resync", args...)
	}
	if err := p.Mappings.WritePending(string(entityType), discordID, state); err != nil {
		return fmt.Errorf("reconcile: live repair %s %s: %w", entityType, discordID, err)
	}
	if err := p.Mappings.Confirm(string(entityType), discordID, stoatID); err != nil {
		return fmt.Errorf("reconcile: live repair %s %s: %w", entityType, discordID, err)
	}
	return nil
}

func reconcileRoleLive(p Params, role canonical.Role, liveRoles []stoat.RoleInfo) error {
	m, err := p.Mappings.Get(string(engine.EntityRole), role.ID)
	if err != nil {
		return fmt.Errorf("reconcile: live get mapping role %s: %w", role.ID, err)
	}
	if !m.Found || m.Status != engine.StatusActive {
		return nil
	}

	live, found := findRole(liveRoles, m.StoatID)
	if !found {
		p.Logger.Warn("reconcile: live role missing from Stoat, skipping (out of scope for this pass)", "discord_id", role.ID, "stoat_id", m.StoatID)
		return nil
	}

	liveStoat := canonical.StoatRole{Name: live.Name, Colour: live.Colour, Hoist: live.Hoist, Rank: live.Rank, Permissions: live.Permissions}
	matches := reflect.DeepEqual(liveStoat, role.ToStoat(p.Logger))

	desiredJSON, err := role.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("reconcile: live marshal role %s: %w", role.ID, err)
	}
	return repairMapping(p, engine.EntityRole, role.ID, m.StoatID, matches, desiredJSON)
}

func findRole(roles []stoat.RoleInfo, stoatID string) (stoat.RoleInfo, bool) {
	for _, r := range roles {
		if r.ID == stoatID {
			return r, true
		}
	}
	return stoat.RoleInfo{}, false
}

// comparableChannel is the subset of a channel's attributes reconciled
// here, compared at the canonical.ChannelType/StoatOverwrite level so no
// wire-string channel-type table (private to internal/canonical) needs
// duplicating -- overwrite bits still go through canonical's own
// StoatOverwrite shape, the same one op builders' Diff closures compare.
type comparableChannel struct {
	Name       string
	Type       canonical.ChannelType
	Overwrites map[string]canonical.StoatOverwrite
}

func reconcileChannelLive(ctx context.Context, p Params, ch canonical.Channel, stoatToDiscordRole map[string]string) error {
	m, err := p.Mappings.Get(string(engine.EntityChannel), ch.ID)
	if err != nil {
		return fmt.Errorf("reconcile: live get mapping channel %s: %w", ch.ID, err)
	}
	if !m.Found || m.Status != engine.StatusActive {
		return nil
	}

	live, err := p.Reader.FetchChannel(ctx, m.StoatID)
	if err != nil {
		p.Logger.Warn("reconcile: live channel fetch failed, skipping (out of scope for this pass)", "discord_id", ch.ID, "stoat_id", m.StoatID, "error", err)
		return nil
	}

	liveOverwrites := make(map[string]canonical.StoatOverwrite, len(live.RolePermissions)+1)
	for stoatRoleID, ow := range live.RolePermissions {
		discordRoleID, ok := stoatToDiscordRole[stoatRoleID]
		if !ok {
			p.Logger.Warn("reconcile: live channel overwrite references unmapped role, excluding from comparison", "channel_discord_id", ch.ID, "stoat_role_id", stoatRoleID)
			continue
		}
		liveOverwrites[discordRoleID] = ow
	}
	if live.DefaultPermissions != nil {
		liveOverwrites[p.GuildID] = *live.DefaultPermissions
	}

	liveState := comparableChannel{Name: live.Name, Type: live.Type, Overwrites: liveOverwrites}
	desiredState := comparableChannel{Name: ch.Name, Type: ch.Type, Overwrites: ch.ToStoat(p.Logger).Overwrites}
	matches := reflect.DeepEqual(liveState, desiredState)

	desiredJSON, err := ch.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("reconcile: live marshal channel %s: %w", ch.ID, err)
	}
	return repairMapping(p, engine.EntityChannel, ch.ID, m.StoatID, matches, desiredJSON)
}

func reconcileCategoryLive(p Params, cat canonical.Category, liveCategories []stoat.CategoryInfo, stoatToDiscordChannel map[string]string) error {
	m, err := p.Mappings.Get(string(engine.EntityCategory), cat.ID)
	if err != nil {
		return fmt.Errorf("reconcile: live get mapping category %s: %w", cat.ID, err)
	}
	if !m.Found || m.Status != engine.StatusActive {
		return nil
	}

	live, liveIndex, found := findCategory(liveCategories, m.StoatID)
	if !found {
		p.Logger.Warn("reconcile: live category missing from Stoat, skipping (out of scope for this pass)", "discord_id", cat.ID, "stoat_id", m.StoatID)
		return nil
	}

	liveChannelIDs := make([]string, 0, len(live.ChannelIDs))
	for _, stoatChannelID := range live.ChannelIDs {
		discordChannelID, ok := stoatToDiscordChannel[stoatChannelID]
		if !ok {
			p.Logger.Warn("reconcile: live category references unmapped channel, excluding from comparison", "category_discord_id", cat.ID, "stoat_channel_id", stoatChannelID)
			continue
		}
		liveChannelIDs = append(liveChannelIDs, discordChannelID)
	}

	// liveIndex is the category's position within Stoat's own live-ordered
	// categories array, mirroring how canonical.Category.Position is
	// derived from Discord's sidebar order -- comparing that against the
	// raw Stoat CategoryInfo (which carries no position field) would
	// otherwise always mismatch once Position is set to anything but 0.
	liveCat := canonical.Category{ID: cat.ID, Name: live.Title, ChannelIDs: liveChannelIDs, Position: liveIndex}
	matches := reflect.DeepEqual(liveCat, cat)

	desiredJSON, err := cat.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("reconcile: live marshal category %s: %w", cat.ID, err)
	}
	return repairMapping(p, engine.EntityCategory, cat.ID, m.StoatID, matches, desiredJSON)
}

func findCategory(categories []stoat.CategoryInfo, stoatID string) (stoat.CategoryInfo, int, bool) {
	for i, c := range categories {
		if c.ID == stoatID {
			return c, i, true
		}
	}
	return stoat.CategoryInfo{}, -1, false
}
