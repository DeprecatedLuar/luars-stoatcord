package discord

import (
	"context"
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
func BuildCategoryOp(kind engine.OpKind, cat canonical.Category, allCategories func() []canonical.Category, stoatServerID string, mappings MappingReader, writer CategoryWriter) engine.Op {
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
			resolved := make([]canonical.Category, 0, len(allCategories()))
			for _, c := range allCategories() {
				stoatChannelIDs, err := resolveChannelIDs(c.ChannelIDs, mappings)
				if err != nil {
					return "", err
				}
				resolved = append(resolved, canonical.Category{ID: c.ID, Name: c.Name, ChannelIDs: stoatChannelIDs})
			}

			if err := writer.SetCategories(ctx, stoatServerID, resolved); err != nil {
				return "", err
			}
			return cat.ID, nil
		},
	}
}

// resolveChannelIDs translates Discord channel ids to their mapped Stoat
// channel ids, preserving order (display position, spec 6). A channel
// missing from the mapping table is skipped defensively -- DependsOn should
// already have gated the op until every member channel is mapped.
func resolveChannelIDs(discordChannelIDs []string, mappings MappingReader) ([]string, error) {
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
			continue
		}
		resolved = append(resolved, m.StoatID)
	}
	return resolved, nil
}
