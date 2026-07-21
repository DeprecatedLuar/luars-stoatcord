package discord

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

// CategoryWriter is the subset of *internal/stoat.Client's category methods
// this package needs. Stoat has no per-category CRUD, so it always takes
// the server's whole categories array (spec 6).
type CategoryWriter interface {
	SetCategories(ctx context.Context, serverID string, categories []canonical.Category) error
}

// BuildCategoryOp translates a Discord category create/update event into an
// engine.Op. allCategories returns every category's current desired state
// (a live snapshot from the caller, e.g. discordgo's guild cache) --
// Apply resends the entire array on any single category's change, since
// Stoat's wire model has no per-category edit. The Stoat category id is the
// Discord category id used directly: Stoat's ServerCategory.Id is
// client-chosen, not server-generated, so no create round-trip is needed to
// learn it.
func BuildCategoryOp(kind engine.OpKind, cat canonical.Category, allCategories func() []canonical.Category, stoatServerID string, mappings MappingReader, writer CategoryWriter, logger *slog.Logger) engine.Op {
	canonicalState, _ := cat.CanonicalJSON()

	dependsOn := make([]engine.DependencyKey, 0, len(cat.ChannelIDs))
	for _, channelID := range cat.ChannelIDs {
		dependsOn = append(dependsOn, engine.DependencyKey{EntityType: engine.EntityChannel, DiscordID: channelID})
	}

	return engine.Op{
		Kind:           kind,
		EntityType:     engine.EntityCategory,
		DiscordID:      cat.ID,
		CanonicalState: string(canonicalState),
		DependsOn:      dependsOn,
		Diff: func(storedCanonicalState string) (bool, error) {
			stored, err := canonical.ParseCategoryCanonicalJSON([]byte(storedCanonicalState))
			if err != nil {
				return false, err
			}
			return reflect.DeepEqual(stored, cat), nil
		},
		Apply: func(ctx context.Context) (string, error) {
			if err := resendCategories(ctx, allCategories, stoatServerID, mappings, writer, logger); err != nil {
				return "", err
			}
			return cat.ID, nil
		},
	}
}

// BuildCategoryDeleteOp translates a Discord category delete event into an
// engine.Op. Stoat has no per-category delete endpoint (CategoryWriter doc
// comment), so "deleting" a category means resending the whole array
// without it -- the same resend BuildCategoryOp's Apply already does.
// Unlike BuildCategoryOp, this is an OpDelete: the engine runs Apply
// unconditionally, skipping the Diff-based change-detection gate entirely
// (internal/engine/engine.go's process()). That gate is what a plain
// re-derived BuildCategoryOp update relies on to fire, but nothing
// represents a removed id in the re-derived list to gate on -- deletion
// must not depend on some surviving sibling's own diff happening to change
// (it doesn't, when the deleted category was last in sidebar order).
// allCategories must be the post-delete snapshot (the caller's state cache
// no longer includes discordCategoryID by the time this fires).
func BuildCategoryDeleteOp(discordCategoryID string, allCategories func() []canonical.Category, stoatServerID string, mappings MappingReader, writer CategoryWriter, logger *slog.Logger) engine.Op {
	return engine.Op{
		Kind:       engine.OpDelete,
		EntityType: engine.EntityCategory,
		DiscordID:  discordCategoryID,
		Apply: func(ctx context.Context) (string, error) {
			return "", resendCategories(ctx, allCategories, stoatServerID, mappings, writer, logger)
		},
	}
}

// resendCategories resolves every category's Discord channel ids to their
// mapped Stoat ids and pushes the whole array in one PATCH (spec 6: Stoat's
// wire model has no per-category edit or delete). Shared by BuildCategoryOp
// and BuildCategoryDeleteOp so both stay behind one implementation.
func resendCategories(ctx context.Context, allCategories func() []canonical.Category, stoatServerID string, mappings MappingReader, writer CategoryWriter, logger *slog.Logger) error {
	resolved := make([]canonical.Category, 0, len(allCategories()))
	for _, c := range allCategories() {
		stoatChannelIDs, err := resolveChannelIDs(c.ChannelIDs, mappings, logger)
		if err != nil {
			return err
		}
		resolved = append(resolved, canonical.Category{ID: c.ID, Name: c.Name, ChannelIDs: stoatChannelIDs})
	}
	return writer.SetCategories(ctx, stoatServerID, resolved)
}

// resolveChannelIDs translates Discord channel ids to their mapped Stoat
// channel ids, preserving order (display position, spec 6). A channel
// missing from the mapping table is skipped -- DependsOn should already
// have gated the op until every member channel is mapped, so this should
// only ever fire on a transient pending row; logged (WARN) rather than
// silent so a partial category list is visible, never a silent loss.
func resolveChannelIDs(discordChannelIDs []string, mappings MappingReader, logger *slog.Logger) ([]string, error) {
	resolved := make([]string, 0, len(discordChannelIDs))
	for _, channelID := range discordChannelIDs {
		m, err := mappings.Get(string(engine.EntityChannel), channelID)
		if err != nil {
			return nil, err
		}
		// Found alone isn't enough: a pending row (create not yet
		// remote-confirmed) has an empty StoatID, which would otherwise
		// get appended and corrupt the categories payload. DependsOn only
		// gates this op's own channels -- Apply resends every category's
		// list, so other categories' still-pending channels can reach
		// here too; skip them the same as unmapped ones.
		if !m.Found || m.Status != engine.StatusActive {
			logger.Warn("discord: category resend skipping unmapped/pending channel", "channel_id", channelID)
			continue
		}
		resolved = append(resolved, m.StoatID)
	}
	return resolved, nil
}
