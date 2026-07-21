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

// applyChannelOverwrites injects the bot's own elevation role self-grant
// first (implementation-plan.md Phase 4.7 guarantee 2 -- unconditional,
// every channel, not only ones whose default overwrite denies
// ViewChannel), before any Discord-derived overwrite. First matters: a
// later @everyone deny in overwrites would otherwise trigger Stoat's
// revoke_all() and 403 the bot out of its own channel before the grant
// ever lands. Skipped only if ResolveElevationRole hasn't cached a role
// yet -- production startup hard-fails before this can happen (guarantee
// 1), so this is a defensive no-op for callers (mainly tests) that never
// resolved one.
func (c *Client) applyChannelOverwrites(ctx context.Context, channelID string, overwrites map[string]canonical.StoatOverwrite) error {
	if c.elevation != nil {
		if err := c.SetChannelPermissions(ctx, channelID, c.elevation.id, GrantAllSafe, 0); err != nil {
			return fmt.Errorf("stoat: inject elevation role self-grant on channel %s: %w", channelID, err)
		}
	}
	for roleID, ow := range overwrites {
		if err := c.SetChannelPermissions(ctx, channelID, roleID, ow.Allow, ow.Deny); err != nil {
			return err
		}
	}
	return nil
}
