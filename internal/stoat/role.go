package stoat

import (
	"context"
	"fmt"

	"github.com/luar/stoatcord/internal/canonical"
	"within.website/x/web/revolt"
)

// CreateRole creates a role (the library's ServerCreateRole only accepts a
// name), then applies the remaining fields and tri-state permissions,
// returning the new role's Stoat id.
func (c *Client) CreateRole(ctx context.Context, serverID string, role canonical.StoatRole) (string, error) {
	id, err := c.inner.ServerCreateRole(ctx, serverID, role.Name)
	if err != nil {
		return "", fmt.Errorf("stoat: create role %q: %w", role.Name, err)
	}
	if err := c.EditRole(ctx, serverID, id, role); err != nil {
		return id, err
	}
	return id, nil
}

// EditRole updates an existing role's fields and tri-state permissions.
// Rank is clamped to at least minimumMirroredRank (implementation-plan.md
// Phase 4.7 guarantee 1, invariant 3a): rank 0 is reserved for the bot's
// own elevation role, so a mirrored role can never collide with or occupy
// it. Refuses outright to write to the elevation role itself (invariant 2:
// the mirror never edits rank 0, no rank change, no permission edit).
func (c *Client) EditRole(ctx context.Context, serverID, roleID string, role canonical.StoatRole) error {
	if c.isElevationRole(roleID) {
		return fmt.Errorf("stoat: refusing to edit role %s, it is the bot's own elevation role", roleID)
	}
	rank := max(role.Rank, minimumMirroredRank)
	edit := (&revolt.EditRole{}).SetColor(role.Colour).IsHoist(role.Hoist).SetRank(rank)
	if err := c.inner.ServerEditRole(ctx, serverID, roleID, edit); err != nil {
		return fmt.Errorf("stoat: edit role %s: %w", roleID, err)
	}
	if err := c.SetRolePermissions(ctx, serverID, roleID, role.Permissions.Allow, role.Permissions.Deny); err != nil {
		return err
	}
	return nil
}

// DeleteRole deletes a role by its Stoat id. Refuses outright to delete the
// bot's own elevation role (implementation-plan.md Phase 4.7 guarantee 1,
// invariant 2).
func (c *Client) DeleteRole(ctx context.Context, serverID, roleID string) error {
	if c.isElevationRole(roleID) {
		return fmt.Errorf("stoat: refusing to delete role %s, it is the bot's own elevation role", roleID)
	}
	if err := c.inner.ServerDeleteRole(ctx, serverID, roleID); err != nil {
		return fmt.Errorf("stoat: delete role %s: %w", roleID, err)
	}
	return nil
}
