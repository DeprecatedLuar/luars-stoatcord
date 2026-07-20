package stoat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/luar/stoatcord/internal/canonical"
)

// rawOverride is a role or channel permission override as Stoat's wire
// format encodes it: keys "a"/"d" (ground truth:
// crates/core/permissions/src/models/server.rs's OverrideField). Distinct
// from permissionsBody in permission.go, which is the *write*-side shape
// ("allow"/"deny") for a different set of endpoints.
type rawOverride struct {
	Allow int64 `json:"a"`
	Deny  int64 `json:"d"`
}

func (o rawOverride) toStoat() canonical.StoatOverwrite {
	return canonical.StoatOverwrite{Allow: uint64(o.Allow), Deny: uint64(o.Deny)}
}

// ServerInfo is a Stoat server's live state: identity fields (name,
// category/channel/role ids) for binding existing entities by name
// (internal/reconcile.Bind), plus each role's full attributes so a live
// reconcile pass can diff them against canonical's desired state.
type ServerInfo struct {
	Name       string
	Categories []CategoryInfo
	ChannelIDs []string
	Roles      []RoleInfo
}

// CategoryInfo is one server-level category as Stoat stores it.
type CategoryInfo struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	ChannelIDs []string `json:"channels"`
}

// RoleInfo is a role's live state, decoded out of the library's untyped
// Server.Roles map[string]any: identity (ID/Name) plus every attribute
// canonical.StoatRole carries, so a live role can be diffed against
// canonical's desired ToStoat() shape.
type RoleInfo struct {
	ID          string
	Name        string
	Colour      string
	Hoist       bool
	Rank        int
	Permissions canonical.StoatOverwrite
}

// ChannelInfo is a channel's live state. FetchServer only returns channel
// ids (the library's Server type carries no per-channel name/type), so
// matching a channel by name and type needs a separate FetchChannel call
// per id. DefaultPermissions/RolePermissions are keyed by raw Stoat ids
// (the literal sentinel "default" for the channel's everyone-overwrite,
// real role ids otherwise) -- translating to/from Discord ids is the
// caller's job, not this package's (internal/stoat never does identity
// translation).
type ChannelInfo struct {
	ID                 string
	Name               string
	Type               canonical.ChannelType
	DefaultPermissions *canonical.StoatOverwrite
	RolePermissions    map[string]canonical.StoatOverwrite
}

// rawChannel decodes just the fields this package needs directly off
// Stoat's channel JSON, bypassing the vendored library's Channel struct --
// which has no type field at all (a fourth library gap beyond the three
// documented in implementation-plan.md, same workaround pattern as gap 2).
// Ground truth (crates/core/database/src/models/channels/model.rs on
// stoatchat/stoatchat): the live schema merged VoiceChannel into TextChannel
// -- every server channel's wire "channel_type" reads "TextChannel"
// regardless of kind, so that field cannot discriminate text vs voice.
// Kind is instead carried by presence of the optional "voice" field.
type rawChannel struct {
	ID                 string                 `json:"_id"`
	Name               string                 `json:"name"`
	Voice              json.RawMessage        `json:"voice,omitempty"`
	DefaultPermissions *rawOverride           `json:"default_permissions,omitempty"`
	RolePermissions    map[string]rawOverride `json:"role_permissions,omitempty"`
}

func (rc rawChannel) channelType() canonical.ChannelType {
	if len(rc.Voice) > 0 && string(rc.Voice) != "null" {
		return canonical.ChannelTypeVoice
	}
	return canonical.ChannelTypeText
}

// rawServer decodes just the fields this package needs directly off
// Stoat's server JSON. Roles stays untyped (map[string]json.RawMessage)
// because only the name is needed here, and the library's own Server.Roles
// (map[string]any) requires the same per-entry re-decode regardless.
type rawServer struct {
	Name       string                     `json:"name"`
	ChannelIDs []string                   `json:"channels"`
	Categories []CategoryInfo             `json:"categories"`
	Roles      map[string]json.RawMessage `json:"roles"`
}

// FetchServer reads a Stoat server's live structure: name, ordered
// categories (each with its channel ids), channel ids, and roles with their
// full attributes.
func (c *Client) FetchServer(ctx context.Context, serverID string) (ServerInfo, error) {
	data, err := c.inner.Request(ctx, "GET", "/servers/"+serverID, []byte{})
	if err != nil {
		return ServerInfo{}, fmt.Errorf("stoat: fetch server %s: %w", serverID, err)
	}

	var raw rawServer
	if err := json.Unmarshal(data, &raw); err != nil {
		return ServerInfo{}, fmt.Errorf("stoat: decode server %s: %w", serverID, err)
	}

	roles := make([]RoleInfo, 0, len(raw.Roles))
	for id, roleData := range raw.Roles {
		var decoded struct {
			Name        string      `json:"name"`
			Colour      string      `json:"colour"`
			Hoist       bool        `json:"hoist"`
			Rank        int         `json:"rank"`
			Permissions rawOverride `json:"permissions"`
		}
		if err := json.Unmarshal(roleData, &decoded); err != nil {
			return ServerInfo{}, fmt.Errorf("stoat: decode role %s on server %s: %w", id, serverID, err)
		}
		roles = append(roles, RoleInfo{
			ID:          id,
			Name:        decoded.Name,
			Colour:      decoded.Colour,
			Hoist:       decoded.Hoist,
			Rank:        decoded.Rank,
			Permissions: decoded.Permissions.toStoat(),
		})
	}

	return ServerInfo{
		Name:       raw.Name,
		Categories: raw.Categories,
		ChannelIDs: raw.ChannelIDs,
		Roles:      roles,
	}, nil
}

// FetchChannel reads a single channel's live state: name, canonical type,
// and its default/role permission overwrites. Needed because FetchServer's
// channel ids carry no attributes of their own.
func (c *Client) FetchChannel(ctx context.Context, channelID string) (ChannelInfo, error) {
	data, err := c.inner.Request(ctx, "GET", "/channels/"+channelID, []byte{})
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("stoat: fetch channel %s: %w", channelID, err)
	}

	var raw rawChannel
	if err := json.Unmarshal(data, &raw); err != nil {
		return ChannelInfo{}, fmt.Errorf("stoat: decode channel %s: %w", channelID, err)
	}

	var defaultPermissions *canonical.StoatOverwrite
	if raw.DefaultPermissions != nil {
		ow := raw.DefaultPermissions.toStoat()
		defaultPermissions = &ow
	}
	rolePermissions := make(map[string]canonical.StoatOverwrite, len(raw.RolePermissions))
	for roleID, ow := range raw.RolePermissions {
		rolePermissions[roleID] = ow.toStoat()
	}

	return ChannelInfo{
		ID:                 raw.ID,
		Name:               raw.Name,
		Type:               raw.channelType(),
		DefaultPermissions: defaultPermissions,
		RolePermissions:    rolePermissions,
	}, nil
}
