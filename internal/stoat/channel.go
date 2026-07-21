package stoat

import (
	"context"
	"fmt"

	"github.com/luar/stoatcord/internal/canonical"
	"within.website/x/web/revolt"
)

// CreateChannel creates a text or voice channel under serverID and applies
// every role overwrite in ch.Overwrites, returning the new channel's Stoat
// id.
func (c *Client) CreateChannel(ctx context.Context, serverID string, ch canonical.StoatChannel) (string, error) {
	var created *revolt.Channel
	var err error
	switch ch.Type {
	case "Voice":
		created, err = c.inner.CreateVoiceChannel(ctx, serverID, ch.Name, "")
	default:
		created, err = c.inner.CreateTextChannel(ctx, serverID, ch.Name, "")
	}
	if err != nil {
		return "", fmt.Errorf("stoat: create channel %q: %w", ch.Name, err)
	}

	if err := c.applyChannelOverwrites(ctx, created.Id, ch.Overwrites); err != nil {
		return created.Id, err
	}
	return created.Id, nil
}

// EditChannel updates an existing channel's name and every role overwrite in
// ch.Overwrites.
func (c *Client) EditChannel(ctx context.Context, channelID string, ch canonical.StoatChannel) error {
	edit := (&revolt.EditChannel{}).SetName(ch.Name)
	if err := c.inner.ChannelEdit(ctx, channelID, edit); err != nil {
		return fmt.Errorf("stoat: edit channel %s: %w", channelID, err)
	}
	return c.applyChannelOverwrites(ctx, channelID, ch.Overwrites)
}

// DeleteChannel deletes a channel by its Stoat id.
func (c *Client) DeleteChannel(ctx context.Context, channelID string) error {
	if err := c.inner.ChannelDelete(ctx, channelID); err != nil {
		return fmt.Errorf("stoat: delete channel %s: %w", channelID, err)
	}
	return nil
}

// applyChannelOverwrites injects a self-grant for every admin-mapped role
// first (implementation-plan.md Phase 4.7 guarantee 2 -- unconditional,
// every channel, not only ones whose default overwrite denies ViewChannel),
// before any Discord-derived overwrite. First matters: a later @everyone
// deny in overwrites would otherwise trigger Stoat's revoke_all() and 403
// the bot out of its own channel before the grant ever lands.
//
// This deliberately targets admin-mapped roles (Discord roles that carried
// ADMINISTRATOR), never the bot's own elevation role: Stoat's
// permissions_set.rs refuses a non-owner writing an override for a role at
// or above their own rank, and the elevation role's rank IS the bot's own
// rank once worn -- that self-grant 403s unconditionally, on every channel,
// confirmed live (crates/delta/src/routes/channels/permissions_set.rs).
// Admin-mapped roles are always ranked worse than the elevation role
// (minimumMirroredRank clamps every mirrored role to >= 1), so the bot can
// write their overrides freely, and since they're normal mirrored roles a
// human holding one gets the same visibility on Stoat that ADMINISTRATOR
// gave them on Discord.
//
// The grant itself is the bot's own elevation role permissions
// (ElevationPermissions), not a theoretical "all permissions" mask: Stoat's
// throw_permission_override (crates/core/permissions/src/models/mod.rs)
// refuses to let an actor grant a bit it doesn't already hold, and a
// blanket mask always includes bits the bot doesn't have (confirmed live --
// even reserved/future bits the bot was never granted). Using the bot's own
// bits is self-consistent by construction: the actor granting them always
// already holds them. The admin role's ceiling is therefore "as powerful as
// the bot", which may fall short of literally every Stoat permission bit
// (e.g. ViewAuditLogs isn't part of this bot's elevation role) -- that's
// the intended tradeoff, not a regression.
func (c *Client) applyChannelOverwrites(ctx context.Context, channelID string, overwrites map[string]canonical.StoatOverwrite) error {
	if len(c.adminRoles) > 0 && c.elevation == nil {
		return fmt.Errorf("stoat: elevation role not resolved, cannot self-grant admin roles on channel %s", channelID)
	}
	for _, roleID := range c.adminRoles {
		if err := c.SetChannelPermissions(ctx, channelID, roleID, c.ElevationPermissions(), 0); err != nil {
			return fmt.Errorf("stoat: inject admin role %s self-grant on channel %s: %w", roleID, channelID, err)
		}
	}
	for roleID, ow := range overwrites {
		if err := c.SetChannelPermissions(ctx, channelID, roleID, ow.Allow, ow.Deny); err != nil {
			return err
		}
	}
	return nil
}
