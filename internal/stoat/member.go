package stoat

import (
	"context"
	"fmt"
	"slices"

	"within.website/x/web/revolt"
)

// AddSelfToRole adds roleID to the bot's own Stoat member roles on serverID,
// preserving every role it already wears -- a no-op if it already holds
// roleID. Confirmed viable against ground truth
// crates/delta/src/routes/servers/member_edit.rs: self-edits skip the
// "can't touch a member ranked >= you" guard, but role additions are still
// checked unconditionally against the actor's own rank. The bot's own rank
// while wearing its elevation role is rank 0 (the best possible), so it can
// always add itself to any normal mirrored role (all clamped to
// >= minimumMirroredRank, see EditRole).
func (c *Client) AddSelfToRole(ctx context.Context, serverID, roleID string) error {
	self, err := c.inner.FetchUser(ctx, "@me")
	if err != nil {
		return fmt.Errorf("stoat: fetch self user: %w", err)
	}
	member, err := c.inner.ServerFetchMember(ctx, serverID, self.Id)
	if err != nil {
		return fmt.Errorf("stoat: fetch self member on server %s: %w", serverID, err)
	}
	if slices.Contains(member.Roles, roleID) {
		return nil
	}

	newRoles := append(slices.Clone(member.Roles), roleID)
	if err := c.inner.ServerEditMember(ctx, serverID, self.Id, &revolt.EditMember{Roles: newRoles}); err != nil {
		return fmt.Errorf("stoat: add self to role %s on server %s: %w", roleID, serverID, err)
	}
	return nil
}
