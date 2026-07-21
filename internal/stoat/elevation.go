package stoat

import (
	"context"
	"fmt"
	"slices"
)

// elevationRank0 is the rank Stoat reserves for the bot's own elevation
// role (implementation-plan.md Phase 4.7 guarantee 1). Stoat ranks ascend
// from 0 at the top -- ground truth
// crates/core/permissions/src/impl.rs's get_ranking/rank check.
const elevationRank0 = 0

// minimumMirroredRank is the lowest rank any Discord-mirrored role may be
// assigned (see EditRole), reserving elevationRank0 for the bot's own
// role so a mirrored role can never collide with or occupy it.
const minimumMirroredRank = 1

// elevationRole is the bot's own resolved elevation role, cached on Client
// by ResolveElevationRole.
type elevationRole struct {
	id          string
	permissions uint64
}

// ResolveElevationRole finds the server's rank-0 role and verifies the bot
// wears it (implementation-plan.md Phase 4.7 guarantee 1, invariant 1):
// without it the bot cannot manage any role or permission at all
// (crates/core/permissions/src/impl.rs bypasses only for
// are_we_server_owner/are_we_privileged, neither of which the bot is --
// see this repo's CLAUDE.md "Bot elevation role"). A violation is returned
// as an error, never silently degraded -- callers should treat it as fatal
// to startup.
//
// On success, the resolved role id is cached and returned by
// ElevationRoleID -- both the channel self-grant injection (Phase 4.7
// guarantee 2) and internal/reconcile's foreign-entity reap exemption
// consume that single cached source instead of re-resolving it themselves.
func (c *Client) ResolveElevationRole(ctx context.Context, serverID string) error {
	server, err := c.FetchServer(ctx, serverID)
	if err != nil {
		return err
	}

	var rank0 []RoleInfo
	for _, r := range server.Roles {
		if r.Rank == elevationRank0 {
			rank0 = append(rank0, r)
		}
	}
	if len(rank0) == 0 {
		return fmt.Errorf("stoat: no role at rank %d on server %s, cannot resolve elevation role", elevationRank0, serverID)
	}
	if len(rank0) > 1 {
		return fmt.Errorf("stoat: %d roles tied at rank %d on server %s, elevation role is ambiguous", len(rank0), elevationRank0, serverID)
	}
	role := rank0[0]

	selfIDs, err := c.FetchSelfRoleIDs(ctx, serverID)
	if err != nil {
		return err
	}
	if !slices.Contains(selfIDs, role.ID) {
		return fmt.Errorf("stoat: bot does not wear rank-%d role %s on server %s", elevationRank0, role.ID, serverID)
	}

	c.elevation = &elevationRole{id: role.ID, permissions: role.Permissions.Allow}
	return nil
}

// ElevationRoleID returns the elevation role id cached by a prior
// successful ResolveElevationRole call, or "" if none has succeeded yet.
func (c *Client) ElevationRoleID() string {
	if c.elevation == nil {
		return ""
	}
	return c.elevation.id
}

// ElevationPermissions returns the bot's own elevation role's live allow-bits,
// cached by a prior successful ResolveElevationRole call, or 0 if none has
// succeeded yet. applyChannelOverwrites (channel.go) uses this instead of a
// theoretical "grant everything" mask: Stoat's throw_permission_override
// (crates/core/permissions/src/models/mod.rs) refuses to let an actor grant
// a bit it doesn't already hold, so only bits the bot actually holds can
// ever be self-granted without a 403.
func (c *Client) ElevationPermissions() uint64 {
	if c.elevation == nil {
		return 0
	}
	return c.elevation.permissions
}

// isElevationRole reports whether roleID is the bot's own cached elevation
// role. Used by EditRole/DeleteRole to refuse writes to it (guarantee 1,
// invariant 2: the mirror never writes to rank 0 -- no rank change, no
// permission edit, no delete). Returns false, not an error, when
// ResolveElevationRole hasn't run yet or cached nothing -- callers already
// treat that state as fatal to startup elsewhere (cmd/stoatcord), so a
// role write attempted before resolution isn't this guard's job to catch.
func (c *Client) isElevationRole(roleID string) bool {
	return c.elevation != nil && c.elevation.id == roleID
}
