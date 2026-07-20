package stoat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/luar/stoatcord/internal/canonical"
)

// ServerInfo is the identity-relevant slice of a Stoat server's live state:
// enough to bind existing entities by name (internal/reconcile) and never
// used for attribute diffing (spec 2 guardrail -- comparison is always
// through canonical, live Stoat reads are identity/foreign-detection only).
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

// RoleInfo is a role's identity-relevant fields, decoded out of the
// library's untyped Server.Roles map[string]any.
type RoleInfo struct {
	ID   string
	Name string
}

// ChannelInfo is a channel's identity-relevant fields. FetchServer only
// returns channel ids (the library's Server type carries no per-channel
// name/type), so matching a channel by name and type needs a separate
// FetchChannel call per id.
type ChannelInfo struct {
	ID   string
	Name string
	Type canonical.ChannelType
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
	ID    string          `json:"_id"`
	Name  string          `json:"name"`
	Voice json.RawMessage `json:"voice,omitempty"`
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

// FetchServer reads a Stoat server's identity-relevant structure: name,
// ordered categories, channel ids, and roles.
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
			Name string `json:"name"`
		}
		if err := json.Unmarshal(roleData, &decoded); err != nil {
			return ServerInfo{}, fmt.Errorf("stoat: decode role %s on server %s: %w", id, serverID, err)
		}
		roles = append(roles, RoleInfo{ID: id, Name: decoded.Name})
	}

	return ServerInfo{
		Name:       raw.Name,
		Categories: raw.Categories,
		ChannelIDs: raw.ChannelIDs,
		Roles:      roles,
	}, nil
}

// FetchChannel reads a single channel's identity-relevant fields (name,
// canonical type). Needed because FetchServer's channel ids carry no
// name/type of their own.
func (c *Client) FetchChannel(ctx context.Context, channelID string) (ChannelInfo, error) {
	data, err := c.inner.Request(ctx, "GET", "/channels/"+channelID, []byte{})
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("stoat: fetch channel %s: %w", channelID, err)
	}

	var raw rawChannel
	if err := json.Unmarshal(data, &raw); err != nil {
		return ChannelInfo{}, fmt.Errorf("stoat: decode channel %s: %w", channelID, err)
	}

	return ChannelInfo{ID: raw.ID, Name: raw.Name, Type: raw.channelType()}, nil
}
