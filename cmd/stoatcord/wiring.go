package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/stoat"
	"github.com/luar/stoatcord/internal/store"
)

// drainEngine waits for wait (normally eng.Wait) to return, or gives up
// after timeout and reports timedOut=true. An op permanently deferred on a
// dependency that never confirms (a stuck remote failure, a stale pending
// mapping row) would otherwise block eng.Wait() forever and wedge shutdown
// on SIGTERM/SIGINT.
func drainEngine(wait func(), timeout time.Duration) (timedOut bool) {
	drained := make(chan struct{})
	go func() {
		wait()
		close(drained)
	}()

	select {
	case <-drained:
		return false
	case <-time.After(timeout):
		return true
	}
}

// mappingStoreAdapter satisfies engine.MappingStore over *store.Store.
// store.Mapping and engine.Mapping are structurally identical but distinct
// named types (internal/store must not import internal/engine), so Get is
// the only method that needs translating; WritePending/Confirm/Remove/
// Enqueue already match their interface signatures via embedding.
type mappingStoreAdapter struct {
	*store.Store
}

// resolveAdminRoles fixes the bot's channel self-lockout (found live:
// its own rank-0 elevation role can never write a permission override for
// itself -- crates/delta/src/routes/channels/permissions_set.rs refuses a
// non-owner touching a role at or above their own rank, and the elevation
// role's rank IS the bot's own rank). Instead, Discord roles that carried
// ADMINISTRATOR (canonical.Role.Privileged) are mirrored, ranked below the
// elevation role, and get their channel overrides injected by
// applyChannelOverwrites (internal/stoat/channel.go) -- the bot, wearing
// rank 0, can freely write overrides for any of them.
//
// Returns every admin role's Stoat id (not just the one the bot ends up
// holding): injecting all of them also restores visibility parity for any
// real human who holds one, matching what ADMINISTRATOR gave them on
// Discord. Errors (hard-fail, caller should exit) if no role carries
// ADMINISTRATOR at all -- there is nothing to proceed with.
func resolveAdminRoles(ctx context.Context, st *store.Store, stoatClient *stoat.Client, serverID string, roles []canonical.Role, logger *slog.Logger) ([]string, error) {
	var adminRoles []canonical.Role
	for _, r := range roles {
		if r.Privileged {
			adminRoles = append(adminRoles, r)
		}
	}
	if len(adminRoles) == 0 {
		return nil, fmt.Errorf("discord: no role carries ADMINISTRATOR, cannot resolve an admin role for channel visibility injection")
	}

	stoatIDs := make([]string, 0, len(adminRoles))
	for _, r := range adminRoles {
		mapping, err := st.Get("role", r.ID)
		if err != nil {
			return nil, fmt.Errorf("store: get role %s: %w", r.ID, err)
		}

		stoatID := mapping.StoatID
		if !mapping.Found || mapping.Status != "active" || mapping.StoatID == "" {
			id, err := stoatClient.CreateRole(ctx, serverID, r.ToStoat(logger))
			if err != nil {
				return nil, fmt.Errorf("stoat: mirror admin role %q: %w", r.Name, err)
			}
			canonicalState, err := r.CanonicalJSON()
			if err != nil {
				return nil, fmt.Errorf("canonical: serialize admin role %q: %w", r.Name, err)
			}
			if err := st.WritePending("role", r.ID, string(canonicalState)); err != nil {
				return nil, fmt.Errorf("store: write pending admin role %q: %w", r.Name, err)
			}
			if err := st.Confirm("role", r.ID, id); err != nil {
				return nil, fmt.Errorf("store: confirm admin role %q: %w", r.Name, err)
			}
			stoatID = id
		}
		stoatIDs = append(stoatIDs, stoatID)
	}

	selfRoleIDs, err := stoatClient.FetchSelfRoleIDs(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("stoat: fetch self roles: %w", err)
	}
	holdsAdminRole := slices.ContainsFunc(stoatIDs, func(id string) bool {
		return slices.Contains(selfRoleIDs, id)
	})
	if !holdsAdminRole {
		// Most-senior-by-Discord-rank admin role, as a reasonable default --
		// any held admin role satisfies the injection's correctness, this is
		// just the least-surprising one to pick.
		target := adminRoles[0]
		for _, r := range adminRoles[1:] {
			if r.Rank < target.Rank {
				target = r
			}
		}
		targetIndex := 0
		for i, r := range adminRoles {
			if r.ID == target.ID {
				targetIndex = i
				break
			}
		}
		if err := stoatClient.AddSelfToRole(ctx, serverID, stoatIDs[targetIndex]); err != nil {
			return nil, fmt.Errorf("stoat: self-assign admin role %q: %w", target.Name, err)
		}
	}

	stoatClient.SetAdminRoleIDs(stoatIDs)
	return stoatIDs, nil
}

func (a mappingStoreAdapter) Get(entityType, discordID string) (engine.Mapping, error) {
	m, err := a.Store.Get(entityType, discordID)
	if err != nil {
		return engine.Mapping{}, err
	}
	return engine.Mapping{
		Found:          m.Found,
		StoatID:        m.StoatID,
		Status:         engine.MappingStatus(m.Status),
		CanonicalState: m.CanonicalState,
	}, nil
}
