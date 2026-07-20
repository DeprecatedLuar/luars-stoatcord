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

// SetChannelPermissions sets a role's tri-state permission overwrite on a
// channel. Pass roleID "" to edit the channel's default (everyone)
// permissions.
func (c *Client) SetChannelPermissions(ctx context.Context, channelID, roleID string, allow, deny uint64) error {
	if roleID == "" {
		roleID = "default"
	}

	var body permissionsBody
	body.Permissions.Allow = allow
	body.Permissions.Deny = deny

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("stoat: marshal permissions body: %w", err)
	}

	_, err = c.inner.Request(ctx, "PUT", "/channels/"+channelID+"/permissions/"+roleID, data)
	if err != nil {
		return fmt.Errorf("stoat: set channel %s permissions for role %s: %w", channelID, roleID, err)
	}
	return nil
}

// SetRolePermissions sets a role's tri-state server-level permissions (gap
// 4b: the library's ServerSetRolePermissions sends the wrong
// {"server":X,"channel":Y} shape; the live API's server.roles[id].permissions
// is tri-state {a: allow, d: deny}, confirmed live -- see
// implementation-history.md).
func (c *Client) SetRolePermissions(ctx context.Context, serverID, roleID string, allow, deny uint64) error {
	var body permissionsBody
	body.Permissions.Allow = allow
	body.Permissions.Deny = deny

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("stoat: marshal role permissions body: %w", err)
	}

	_, err = c.inner.Request(ctx, "PUT", "/servers/"+serverID+"/permissions/"+roleID, data)
	if err != nil {
		return fmt.Errorf("stoat: set server %s role %s permissions: %w", serverID, roleID, err)
	}
	return nil
}
