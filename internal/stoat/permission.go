package stoat

import (
	"context"
	"encoding/json"
	"fmt"
)

// permissionsBody is the tri-state shape the live API actually expects
// (gap 2: the library's ChannelSetPermissions sends a flat {"permissions":N}
// uint, which the live API rejects for overwrites -- it needs allow/deny
// separately). Bypasses the library helper and calls Client.Request
// directly.
type permissionsBody struct {
	Permissions struct {
		Allow uint64 `json:"allow"`
		Deny  uint64 `json:"deny"`
	} `json:"permissions"`
}

// defaultRoleID is the sentinel role ID for a channel's default (everyone)
// permission overwrite.
const defaultRoleID = "default"

// GrantAllSafe is Stoat's own "all safe permissions" bitmask (ground truth
// crates/core/permissions/src/models/channel.rs) -- the exact equivalent
// of Discord's Admin bypass, scoped to a single self-override rather than
// a true server-owner grant. applyChannelOverwrites (channel.go) injects
// it for the bot's own elevation role on every channel
// (implementation-plan.md Phase 4.7 guarantee 2), so the bot can never
// lock itself out of a channel it manages.
const GrantAllSafe uint64 = 0x000F_FFFF_FFFF_FFFF

// putPermissions PUTs a tri-state permissions body to path (gap 2 / gap 4b:
// the library's own helpers send the wrong shape for both channel and
// server-role permission endpoints; both are bypassed in favor of a direct
// Client.Request call with this body).
func (c *Client) putPermissions(ctx context.Context, path string, allow, deny uint64) error {
	var body permissionsBody
	body.Permissions.Allow = allow
	body.Permissions.Deny = deny

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("stoat: marshal permissions body: %w", err)
	}

	if _, err := c.inner.Request(ctx, "PUT", path, data); err != nil {
		return fmt.Errorf("stoat: set permissions at %s: %w", path, err)
	}
	return nil
}

// SetChannelPermissions sets a role's tri-state permission overwrite on a
// channel. Pass roleID "" to edit the channel's default (everyone)
// permissions.
func (c *Client) SetChannelPermissions(ctx context.Context, channelID, roleID string, allow, deny uint64) error {
	if roleID == "" {
		roleID = defaultRoleID
	}
	return c.putPermissions(ctx, "/channels/"+channelID+"/permissions/"+roleID, allow, deny)
}

// SetRolePermissions sets a role's tri-state server-level permissions.
func (c *Client) SetRolePermissions(ctx context.Context, serverID, roleID string, allow, deny uint64) error {
	return c.putPermissions(ctx, "/servers/"+serverID+"/permissions/"+roleID, allow, deny)
}
