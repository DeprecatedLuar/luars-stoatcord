package stoat

import (
	"context"
	"fmt"

	"github.com/luar/stoatcord/internal/canonical"
	"within.website/x/web/revolt"
)

// EditServer updates the server's own name/description metadata.
func (c *Client) EditServer(ctx context.Context, serverID string, meta canonical.StoatServer) error {
	edit := (&revolt.EditServer{}).SetName(meta.Name).SetDescription(meta.Description)
	if err := c.inner.ServerEdit(ctx, serverID, edit); err != nil {
		return fmt.Errorf("stoat: edit server %s: %w", serverID, err)
	}
	return nil
}

// SetCategories replaces the server's entire categories array in one PATCH
// (spec 6: category is a server-level ordered channel_ids list, and Stoat's
// wire model has no per-category CRUD -- editing means resubmitting the
// whole list, never a single-category edit).
func (c *Client) SetCategories(ctx context.Context, serverID string, categories []canonical.Category) error {
	edit := &revolt.EditServer{}
	for _, cat := range categories {
		edit.AddCategory(&revolt.ServerCategory{
			Id:         cat.ID,
			Title:      cat.Name,
			ChannelIds: cat.ChannelIDs,
		})
	}
	if err := c.inner.ServerEdit(ctx, serverID, edit); err != nil {
		return fmt.Errorf("stoat: set categories on server %s: %w", serverID, err)
	}
	return nil
}
