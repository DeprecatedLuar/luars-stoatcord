package discord

import (
	"bytes"
	"context"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

type fakeCategoryWriter struct {
	serverID string
	sent     []canonical.Category
}

func (f *fakeCategoryWriter) SetCategories(ctx context.Context, serverID string, categories []canonical.Category) error {
	f.serverID = serverID
	f.sent = categories
	return nil
}

func TestBuildCategoryOp_ApplyResendsWholeListWithResolvedChannelIDs(t *testing.T) {
	general := canonical.Category{ID: "cat1", Name: "General", ChannelIDs: []string{"chan1", "chan2"}}
	voice := canonical.Category{ID: "cat2", Name: "Voice", ChannelIDs: []string{"chan3"}}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})
	mappings.set(string(engine.EntityChannel), "chan2", engine.Mapping{Found: true, StoatID: "stoat-chan2", Status: engine.StatusActive})
	mappings.set(string(engine.EntityChannel), "chan3", engine.Mapping{Found: true, StoatID: "stoat-chan3", Status: engine.StatusActive})

	writer := &fakeCategoryWriter{}
	allCategories := func() []canonical.Category { return []canonical.Category{general, voice} }

	var buf bytes.Buffer
	op := BuildCategoryOp(engine.OpUpdate, general, allCategories, "srv1", mappings, writer, newTestLogger(&buf))

	if len(op.DependsOn) != 2 {
		t.Fatalf("DependsOn = %+v, want 2 entries", op.DependsOn)
	}

	id, err := op.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if id != "cat1" {
		t.Fatalf("id = %q, want cat1 (discord id used directly, Stoat category ids are client-chosen)", id)
	}

	if writer.serverID != "srv1" || len(writer.sent) != 2 {
		t.Fatalf("SetCategories got serverID=%q categories=%+v", writer.serverID, writer.sent)
	}
	if writer.sent[0].ID != "cat1" || writer.sent[0].ChannelIDs[0] != "stoat-chan1" || writer.sent[0].ChannelIDs[1] != "stoat-chan2" {
		t.Fatalf("sent[0] = %+v, want resolved stoat channel ids in order", writer.sent[0])
	}
	if writer.sent[1].ID != "cat2" || writer.sent[1].ChannelIDs[0] != "stoat-chan3" {
		t.Fatalf("sent[1] = %+v, want resolved stoat channel ids", writer.sent[1])
	}
}

// DependsOn only gates the triggering category's own channels, but Apply
// resends every category's channel list (Stoat has no per-category edit).
// A channel belonging to a different category can still be pending
// (Found=true, StoatID="") when this runs; resolveChannelIDs must skip it
// like an unmapped channel instead of appending the empty StoatID.
func TestBuildCategoryOp_ApplySkipsPendingChannelsInOtherCategories(t *testing.T) {
	general := canonical.Category{ID: "cat1", Name: "General", ChannelIDs: []string{"chan1"}}
	voice := canonical.Category{ID: "cat2", Name: "Voice", ChannelIDs: []string{"chan2"}}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})
	mappings.set(string(engine.EntityChannel), "chan2", engine.Mapping{Found: true, StoatID: "", Status: engine.StatusPending})

	writer := &fakeCategoryWriter{}
	allCategories := func() []canonical.Category { return []canonical.Category{general, voice} }

	var buf bytes.Buffer
	op := BuildCategoryOp(engine.OpUpdate, general, allCategories, "srv1", mappings, writer, newTestLogger(&buf))

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(writer.sent) != 2 {
		t.Fatalf("sent = %+v, want 2 categories", writer.sent)
	}
	if len(writer.sent[1].ChannelIDs) != 0 {
		t.Fatalf("sent[1].ChannelIDs = %v, want empty (pending channel must be skipped, not sent as \"\")", writer.sent[1].ChannelIDs)
	}
}

func TestBuildCategoryOp_DiffComparesNameAndChannelOrder(t *testing.T) {
	cat := canonical.Category{ID: "cat1", Name: "General", ChannelIDs: []string{"chan1", "chan2"}}
	var buf bytes.Buffer
	op := BuildCategoryOp(engine.OpUpdate, cat, func() []canonical.Category { return nil }, "srv1", newFakeMappingReader(), &fakeCategoryWriter{}, newTestLogger(&buf))

	sameJSON, err := cat.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	equal, err := op.Diff(string(sameJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !equal {
		t.Fatalf("Diff reported not-equal for identical categories")
	}

	reordered := canonical.Category{ID: "cat1", Name: "General", ChannelIDs: []string{"chan2", "chan1"}}
	reorderedJSON, err := reordered.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	equal, err = op.Diff(string(reorderedJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if equal {
		t.Fatalf("Diff reported equal for categories with different channel order (order is display position)")
	}
}

// A category's own fields (ID/Name/ChannelIDs) can be untouched while its
// Position among sibling categories changes -- e.g. dragging a different
// category above it in the Discord sidebar. Diff must still detect this so
// the resend carrying the corrected order actually fires.
func TestBuildCategoryOp_DiffDetectsPositionChange(t *testing.T) {
	cat := canonical.Category{ID: "cat1", Name: "General", ChannelIDs: []string{"chan1"}, Position: 0}
	var buf bytes.Buffer
	op := BuildCategoryOp(engine.OpUpdate, cat, func() []canonical.Category { return nil }, "srv1", newFakeMappingReader(), &fakeCategoryWriter{}, newTestLogger(&buf))

	movedJSON, err := (canonical.Category{ID: "cat1", Name: "General", ChannelIDs: []string{"chan1"}, Position: 1}).CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	equal, err := op.Diff(string(movedJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if equal {
		t.Fatalf("Diff reported equal for categories with different Position (reorder must be detected)")
	}
}

// The regression case: deleting the category that was last in sidebar
// order leaves no surviving sibling whose Position changes, so nothing
// would ever detect a state change through BuildCategoryOp's Diff gate.
// BuildCategoryDeleteOp must resend unconditionally regardless.
func TestBuildCategoryDeleteOp_ApplyResendsWithoutDeletedCategory(t *testing.T) {
	general := canonical.Category{ID: "cat1", Name: "General", ChannelIDs: []string{"chan1"}, Position: 0}
	// "voice" is the deleted category; allCategories is the post-delete
	// snapshot, so only "general" remains and its own Position is unchanged.
	allCategories := func() []canonical.Category { return []canonical.Category{general} }

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeCategoryWriter{}
	var buf bytes.Buffer
	op := BuildCategoryDeleteOp("cat2", allCategories, "srv1", mappings, writer, newTestLogger(&buf))

	if op.Kind != engine.OpDelete || op.EntityType != engine.EntityCategory || op.DiscordID != "cat2" {
		t.Fatalf("op = %+v", op)
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.serverID != "srv1" || len(writer.sent) != 1 || writer.sent[0].ID != "cat1" {
		t.Fatalf("SetCategories got serverID=%q categories=%+v, want just cat1", writer.serverID, writer.sent)
	}
}
