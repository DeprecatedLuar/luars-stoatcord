package discord

import (
	"bytes"
	"context"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

type fakeMappingReader struct {
	rows map[string]map[string]engine.Mapping
}

func newFakeMappingReader() *fakeMappingReader {
	return &fakeMappingReader{rows: map[string]map[string]engine.Mapping{}}
}

func (f *fakeMappingReader) set(entityType, discordID string, m engine.Mapping) {
	if f.rows[entityType] == nil {
		f.rows[entityType] = map[string]engine.Mapping{}
	}
	f.rows[entityType][discordID] = m
}

func (f *fakeMappingReader) Get(entityType, discordID string) (engine.Mapping, error) {
	return f.rows[entityType][discordID], nil
}

type fakeChannelWriter struct {
	createServerID string
	createCh       canonical.StoatChannel
	createReturns  string

	editChannelID string
	editCh        canonical.StoatChannel

	deleteChannelID string
}

func (f *fakeChannelWriter) CreateChannel(ctx context.Context, serverID string, ch canonical.StoatChannel) (string, error) {
	f.createServerID = serverID
	f.createCh = ch
	return f.createReturns, nil
}

func (f *fakeChannelWriter) EditChannel(ctx context.Context, channelID string, ch canonical.StoatChannel) error {
	f.editChannelID = channelID
	f.editCh = ch
	return nil
}

func (f *fakeChannelWriter) DeleteChannel(ctx context.Context, channelID string) error {
	f.deleteChannelID = channelID
	return nil
}

func TestBuildChannelOp_PopulatesDependsOnFromOverwriteRoles(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{
		ID:   "chan1",
		Name: "general",
		Type: discordgo.ChannelTypeGuildText,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{ID: "role1", Type: discordgo.PermissionOverwriteTypeRole, Allow: 1 << 10},
		},
	}

	op, ok := BuildChannelOp(engine.OpCreate, ch, "srv1", newFakeMappingReader(), &fakeChannelWriter{}, logger)
	if !ok {
		t.Fatalf("BuildChannelOp returned ok=false for a supported channel type")
	}

	if len(op.DependsOn) != 1 || op.DependsOn[0] != (engine.DependencyKey{EntityType: engine.EntityRole, DiscordID: "role1"}) {
		t.Fatalf("DependsOn = %+v, want [{role role1}]", op.DependsOn)
	}
}

func TestBuildChannelOp_UnsupportedChannelTypeReturnsFalse(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{ID: "cat1", Name: "Category", Type: discordgo.ChannelTypeGuildCategory}

	_, ok := BuildChannelOp(engine.OpCreate, ch, "srv1", newFakeMappingReader(), &fakeChannelWriter{}, logger)
	if ok {
		t.Fatalf("BuildChannelOp returned ok=true for a category channel type")
	}
}

func TestBuildChannelOp_ApplyCreatesWhenNotMapped(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{ID: "chan1", Name: "general", Type: discordgo.ChannelTypeGuildText}

	writer := &fakeChannelWriter{createReturns: "stoat-chan1"}
	op, ok := BuildChannelOp(engine.OpCreate, ch, "srv1", newFakeMappingReader(), writer, logger)
	if !ok {
		t.Fatalf("BuildChannelOp ok=false")
	}

	id, err := op.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if id != "stoat-chan1" {
		t.Fatalf("id = %q, want stoat-chan1", id)
	}
	if writer.createServerID != "srv1" || writer.createCh.Name != "general" {
		t.Fatalf("CreateChannel got serverID=%q ch=%+v", writer.createServerID, writer.createCh)
	}
}

func TestBuildChannelOp_ApplyEditsWhenAlreadyMapped(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{ID: "chan1", Name: "renamed", Type: discordgo.ChannelTypeGuildText}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeChannelWriter{}
	op, ok := BuildChannelOp(engine.OpUpdate, ch, "srv1", mappings, writer, logger)
	if !ok {
		t.Fatalf("BuildChannelOp ok=false")
	}

	id, err := op.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if id != "stoat-chan1" {
		t.Fatalf("id = %q, want stoat-chan1", id)
	}
	if writer.editChannelID != "stoat-chan1" || writer.editCh.Name != "renamed" {
		t.Fatalf("EditChannel got channelID=%q ch=%+v", writer.editChannelID, writer.editCh)
	}
}

func TestBuildChannelOp_ApplyResolvesOverwriteRoleIDsToStoatIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{
		ID:   "chan1",
		Name: "general",
		Type: discordgo.ChannelTypeGuildText,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{ID: "role1", Type: discordgo.PermissionOverwriteTypeRole, Allow: 1 << 10},
		},
	}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityRole), "role1", engine.Mapping{Found: true, StoatID: "stoat-role1", Status: engine.StatusActive})

	writer := &fakeChannelWriter{createReturns: "stoat-chan1"}
	op, ok := BuildChannelOp(engine.OpCreate, ch, "srv1", mappings, writer, logger)
	if !ok {
		t.Fatalf("BuildChannelOp ok=false")
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, ok := writer.createCh.Overwrites["role1"]; ok {
		t.Fatalf("Overwrites still keyed by discord role id: %+v", writer.createCh.Overwrites)
	}
	if _, ok := writer.createCh.Overwrites["stoat-role1"]; !ok {
		t.Fatalf("Overwrites not resolved to stoat role id: %+v", writer.createCh.Overwrites)
	}
}

func TestBuildChannelOp_DiffComparesStoatShapeNotRawCanonicalJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{ID: "chan1", Name: "general", Type: discordgo.ChannelTypeGuildText}

	op, ok := BuildChannelOp(engine.OpUpdate, ch, "srv1", newFakeMappingReader(), &fakeChannelWriter{}, logger)
	if !ok {
		t.Fatalf("BuildChannelOp ok=false")
	}

	desired := canonical.Channel{ID: "chan1", Name: "general", Type: canonical.ChannelTypeText}
	storedJSON, err := desired.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}

	equal, err := op.Diff(string(storedJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !equal {
		t.Fatalf("Diff reported not-equal for identical channels")
	}

	changed := canonical.Channel{ID: "chan1", Name: "different-name", Type: canonical.ChannelTypeText}
	changedJSON, err := changed.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	equal, err = op.Diff(string(changedJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if equal {
		t.Fatalf("Diff reported equal for channels with different names")
	}
}

func TestBuildChannelDeleteOp_ApplyDeletesUsingMappedStoatID(t *testing.T) {
	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeChannelWriter{}
	op := BuildChannelDeleteOp("chan1", mappings, writer)

	if op.Kind != engine.OpDelete || op.EntityType != engine.EntityChannel || op.DiscordID != "chan1" {
		t.Fatalf("op = %+v", op)
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.deleteChannelID != "stoat-chan1" {
		t.Fatalf("deleteChannelID = %q, want stoat-chan1", writer.deleteChannelID)
	}
}

func TestBuildChannelDeleteOp_ApplyNoOpWhenNotMapped(t *testing.T) {
	writer := &fakeChannelWriter{}
	op := BuildChannelDeleteOp("chan1", newFakeMappingReader(), writer)

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.deleteChannelID != "" {
		t.Fatalf("deleteChannelID = %q, want empty (no mapping, nothing to delete)", writer.deleteChannelID)
	}
}

// A pending mapping (create written but not yet remote-confirmed) has
// Found=true but an empty StoatID. Apply must not use it -- doing so sent a
// live PATCH/DELETE to "/channels/" with no id and 404'd. The engine now
// serializes same-entity ops so this should only ever be hit by a stale
// pending row left over from a crash, not a live in-flight create.
func TestBuildChannelOp_ApplyFallsBackToCreateWhenMappingPending(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{ID: "chan1", Name: "test", Type: discordgo.ChannelTypeGuildText}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "", Status: engine.StatusPending})

	writer := &fakeChannelWriter{createReturns: "stoat-chan1"}
	op, ok := BuildChannelOp(engine.OpUpdate, ch, "srv1", mappings, writer, logger)
	if !ok {
		t.Fatalf("BuildChannelOp ok=false")
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.editChannelID != "" {
		t.Fatalf("EditChannel called with id=%q while mapping was pending, want it never called", writer.editChannelID)
	}
	if writer.createServerID == "" {
		t.Fatalf("expected safe fallback to CreateChannel, but it was never called")
	}
}

// A row can be Status=pending with a non-empty StoatID: process() (engine)
// always calls WritePending -- which flips status to pending but preserves
// the existing stoat_id -- immediately before calling Apply. So on every
// live update to an already-bound entity, Apply's own mapping read always
// observes status=pending even though the entity genuinely already exists
// on Stoat. Apply must key off StoatID presence, not Status, or it creates
// a duplicate and clobbers the mapping's stoat_id every single time an
// existing entity is updated (found via an isolated reproduction using the
// real engine+store+BuildChannelOp, not a mock).
func TestBuildChannelOp_ApplyEditsWhenMappingPendingButHasStoatID(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{ID: "chan1", Name: "renamed", Type: discordgo.ChannelTypeGuildText}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusPending})

	writer := &fakeChannelWriter{}
	op, ok := BuildChannelOp(engine.OpUpdate, ch, "srv1", mappings, writer, logger)
	if !ok {
		t.Fatalf("BuildChannelOp ok=false")
	}

	id, err := op.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if id != "stoat-chan1" {
		t.Fatalf("id = %q, want stoat-chan1", id)
	}
	if writer.editChannelID != "stoat-chan1" || writer.editCh.Name != "renamed" {
		t.Fatalf("EditChannel got channelID=%q ch=%+v, want it called with the existing stoat id", writer.editChannelID, writer.editCh)
	}
	if writer.createServerID != "" {
		t.Fatalf("CreateChannel was called (serverID=%q) for an entity that already has a stoat id -- this creates a duplicate", writer.createServerID)
	}
}

func TestBuildChannelDeleteOp_ApplyNoOpWhenMappingPending(t *testing.T) {
	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "", Status: engine.StatusPending})

	writer := &fakeChannelWriter{}
	op := BuildChannelDeleteOp("chan1", mappings, writer)

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.deleteChannelID != "" {
		t.Fatalf("DeleteChannel called with id=%q while mapping was pending, want it never called", writer.deleteChannelID)
	}
}
