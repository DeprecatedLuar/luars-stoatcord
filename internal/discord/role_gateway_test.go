package discord

import (
	"bytes"
	"context"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

type fakeRoleWriter struct {
	createServerID string
	createRole     canonical.StoatRole
	createReturns  string

	editServerID string
	editRoleID   string
	editRole     canonical.StoatRole

	deleteServerID string
	deleteRoleID   string
}

func (f *fakeRoleWriter) CreateRole(ctx context.Context, serverID string, role canonical.StoatRole) (string, error) {
	f.createServerID = serverID
	f.createRole = role
	return f.createReturns, nil
}

func (f *fakeRoleWriter) EditRole(ctx context.Context, serverID, roleID string, role canonical.StoatRole) error {
	f.editServerID = serverID
	f.editRoleID = roleID
	f.editRole = role
	return nil
}

func (f *fakeRoleWriter) DeleteRole(ctx context.Context, serverID, roleID string) error {
	f.deleteServerID = serverID
	f.deleteRoleID = roleID
	return nil
}

func TestBuildRoleOp_ApplyCreatesWhenNotMapped(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{ID: "role1", Name: "Moderator", Color: 0xFF00AA, Hoist: true, Position: 5}

	writer := &fakeRoleWriter{createReturns: "stoat-role1"}
	op := BuildRoleOp(engine.OpCreate, r, "srv1", newFakeMappingReader(), writer, logger)

	id, err := op.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if id != "stoat-role1" {
		t.Fatalf("id = %q, want stoat-role1", id)
	}
	if writer.createServerID != "srv1" || writer.createRole.Name != "Moderator" {
		t.Fatalf("CreateRole got serverID=%q role=%+v", writer.createServerID, writer.createRole)
	}
}

func TestBuildRoleOp_ApplyEditsWhenAlreadyMapped(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{ID: "role1", Name: "Renamed"}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityRole), "role1", engine.Mapping{Found: true, StoatID: "stoat-role1", Status: engine.StatusActive})

	writer := &fakeRoleWriter{}
	op := BuildRoleOp(engine.OpUpdate, r, "srv1", mappings, writer, logger)

	id, err := op.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if id != "stoat-role1" {
		t.Fatalf("id = %q, want stoat-role1", id)
	}
	if writer.editServerID != "srv1" || writer.editRoleID != "stoat-role1" || writer.editRole.Name != "Renamed" {
		t.Fatalf("EditRole got serverID=%q roleID=%q role=%+v", writer.editServerID, writer.editRoleID, writer.editRole)
	}
}

func TestBuildRoleOp_DiffComparesStoatShape(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{ID: "role1", Name: "Moderator"}

	op := BuildRoleOp(engine.OpUpdate, r, "srv1", newFakeMappingReader(), &fakeRoleWriter{}, logger)

	desired := canonical.Role{ID: "role1", Name: "Moderator", Colour: "#000000"}
	storedJSON, err := desired.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	equal, err := op.Diff(string(storedJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !equal {
		t.Fatalf("Diff reported not-equal for identical roles")
	}

	changed := canonical.Role{ID: "role1", Name: "Different", Colour: "#000000"}
	changedJSON, err := changed.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	equal, err = op.Diff(string(changedJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if equal {
		t.Fatalf("Diff reported equal for roles with different names")
	}
}

func TestBuildRoleDeleteOp_ApplyDeletesUsingMappedStoatID(t *testing.T) {
	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityRole), "role1", engine.Mapping{Found: true, StoatID: "stoat-role1", Status: engine.StatusActive})

	writer := &fakeRoleWriter{}
	op := BuildRoleDeleteOp("role1", "srv1", mappings, writer)

	if op.Kind != engine.OpDelete || op.EntityType != engine.EntityRole || op.DiscordID != "role1" {
		t.Fatalf("op = %+v", op)
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.deleteServerID != "srv1" || writer.deleteRoleID != "stoat-role1" {
		t.Fatalf("deleteServerID=%q deleteRoleID=%q", writer.deleteServerID, writer.deleteRoleID)
	}
}

func TestBuildRoleDeleteOp_ApplyNoOpWhenNotMapped(t *testing.T) {
	writer := &fakeRoleWriter{}
	op := BuildRoleDeleteOp("role1", "srv1", newFakeMappingReader(), writer)

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.deleteRoleID != "" {
		t.Fatalf("deleteRoleID = %q, want empty (no mapping, nothing to delete)", writer.deleteRoleID)
	}
}

// See the equivalent channel test: a pending mapping has an empty StoatID
// and must never reach EditRole/DeleteRole.
func TestBuildRoleOp_ApplyFallsBackToCreateWhenMappingPending(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{ID: "role1", Name: "test"}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityRole), "role1", engine.Mapping{Found: true, StoatID: "", Status: engine.StatusPending})

	writer := &fakeRoleWriter{createReturns: "stoat-role1"}
	op := BuildRoleOp(engine.OpUpdate, r, "srv1", mappings, writer, logger)

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.editRoleID != "" {
		t.Fatalf("EditRole called with id=%q while mapping was pending, want it never called", writer.editRoleID)
	}
	if writer.createServerID == "" {
		t.Fatalf("expected safe fallback to CreateRole, but it was never called")
	}
}

// See the equivalent channel test (gateway_test.go) for why this case
// matters: process() always calls WritePending -- flipping status to
// pending but preserving stoat_id -- immediately before Apply, so Apply's
// own mapping read always sees status=pending on every live update to an
// already-bound role, even though it genuinely already exists on Stoat.
// Keying off Status instead of StoatID presence means every update to an
// existing role creates a duplicate and clobbers the mapping's stoat_id.
func TestBuildRoleOp_ApplyEditsWhenMappingPendingButHasStoatID(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{ID: "role1", Name: "Renamed"}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityRole), "role1", engine.Mapping{Found: true, StoatID: "stoat-role1", Status: engine.StatusPending})

	writer := &fakeRoleWriter{}
	op := BuildRoleOp(engine.OpUpdate, r, "srv1", mappings, writer, logger)

	id, err := op.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if id != "stoat-role1" {
		t.Fatalf("id = %q, want stoat-role1", id)
	}
	if writer.editServerID != "srv1" || writer.editRoleID != "stoat-role1" || writer.editRole.Name != "Renamed" {
		t.Fatalf("EditRole got serverID=%q roleID=%q role=%+v, want it called with the existing stoat id", writer.editServerID, writer.editRoleID, writer.editRole)
	}
	if writer.createServerID != "" {
		t.Fatalf("CreateRole was called (serverID=%q) for an entity that already has a stoat id -- this creates a duplicate", writer.createServerID)
	}
}

func TestBuildRoleDeleteOp_ApplyNoOpWhenMappingPending(t *testing.T) {
	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityRole), "role1", engine.Mapping{Found: true, StoatID: "", Status: engine.StatusPending})

	writer := &fakeRoleWriter{}
	op := BuildRoleDeleteOp("role1", "srv1", mappings, writer)

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.deleteRoleID != "" {
		t.Fatalf("DeleteRole called with id=%q while mapping was pending, want it never called", writer.deleteRoleID)
	}
}
