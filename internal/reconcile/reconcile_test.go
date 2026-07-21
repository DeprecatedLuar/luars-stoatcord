package reconcile

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/stoat"
)

type fakeMappings struct {
	rows map[string]engine.Mapping // key: entityType+"/"+discordID
}

func newFakeMappings() *fakeMappings {
	return &fakeMappings{rows: map[string]engine.Mapping{}}
}

func (f *fakeMappings) key(entityType, discordID string) string { return entityType + "/" + discordID }

func (f *fakeMappings) Get(entityType, discordID string) (engine.Mapping, error) {
	return f.rows[f.key(entityType, discordID)], nil
}

func (f *fakeMappings) WritePending(entityType, discordID, canonicalState string) error {
	m := f.rows[f.key(entityType, discordID)]
	m.Found = true
	m.Status = engine.StatusPending
	m.CanonicalState = canonicalState
	f.rows[f.key(entityType, discordID)] = m
	return nil
}

func (f *fakeMappings) Confirm(entityType, discordID, stoatID string) error {
	m := f.rows[f.key(entityType, discordID)]
	m.StoatID = stoatID
	m.Status = engine.StatusActive
	f.rows[f.key(entityType, discordID)] = m
	return nil
}

func (f *fakeMappings) Remove(entityType, discordID string) error {
	delete(f.rows, f.key(entityType, discordID))
	return nil
}

type fakeReader struct {
	server      stoat.ServerInfo
	channels    map[string]stoat.ChannelInfo
	channelErrs map[string]error
	selfRoleIDs []string
	selfRoleErr error
}

func (f *fakeReader) FetchServer(ctx context.Context, serverID string) (stoat.ServerInfo, error) {
	return f.server, nil
}

func (f *fakeReader) FetchChannel(ctx context.Context, channelID string) (stoat.ChannelInfo, error) {
	if err, ok := f.channelErrs[channelID]; ok {
		return stoat.ChannelInfo{}, err
	}
	return f.channels[channelID], nil
}

func (f *fakeReader) FetchSelfRoleIDs(ctx context.Context, serverID string) ([]string, error) {
	return f.selfRoleIDs, f.selfRoleErr
}

type fakeWriter struct {
	deletedChannels []string
	deletedRoles    []string
	deleteErr       error
}

func (f *fakeWriter) DeleteChannel(ctx context.Context, channelID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedChannels = append(f.deletedChannels, channelID)
	return nil
}

func (f *fakeWriter) DeleteRole(ctx context.Context, serverID, roleID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedRoles = append(f.deletedRoles, roleID)
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestBind_UniqueNameMatch_WritesActiveMapping(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Categories: []stoat.CategoryInfo{{ID: "stoat-cat-1", Title: "General"}},
			Roles:      []stoat.RoleInfo{{ID: "stoat-role-1", Name: "Admin"}},
			ChannelIDs: []string{"stoat-chan-1"},
		},
		channels: map[string]stoat.ChannelInfo{
			"stoat-chan-1": {ID: "stoat-chan-1", Name: "general", Type: canonical.ChannelTypeText},
		},
	}

	err := Bind(context.Background(), Params{
		ServerID:   "srv1",
		Categories: []canonical.Category{{ID: "dc-cat-1", Name: "General"}},
		Channels:   []canonical.Channel{{ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText}},
		Roles:      []canonical.Role{{ID: "dc-role-1", Name: "Admin"}},
		Mappings:   mappings,
		Reader:     reader,
		DryRun:     true,
		Logger:     testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	catMapping, _ := mappings.Get("category", "dc-cat-1")
	if !catMapping.Found || catMapping.Status != engine.StatusActive || catMapping.StoatID != "stoat-cat-1" {
		t.Fatalf("category mapping = %+v, want bound to stoat-cat-1", catMapping)
	}
	chanMapping, _ := mappings.Get("channel", "dc-chan-1")
	if !chanMapping.Found || chanMapping.StoatID != "stoat-chan-1" {
		t.Fatalf("channel mapping = %+v, want bound to stoat-chan-1", chanMapping)
	}
	roleMapping, _ := mappings.Get("role", "dc-role-1")
	if !roleMapping.Found || roleMapping.StoatID != "stoat-role-1" {
		t.Fatalf("role mapping = %+v, want bound to stoat-role-1", roleMapping)
	}
}

func TestBind_AlreadyMappedIsANoOp_Idempotent(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Categories: []stoat.CategoryInfo{{ID: "stoat-cat-1", Title: "General"}},
		},
	}

	// First run binds.
	params := Params{
		ServerID:   "srv1",
		Categories: []canonical.Category{{ID: "dc-cat-1", Name: "General"}},
		Mappings:   mappings,
		Reader:     reader,
		DryRun:     true,
		Logger:     testLogger(),
	}
	if err := Bind(context.Background(), params); err != nil {
		t.Fatalf("first Bind: %v", err)
	}
	first, _ := mappings.Get("category", "dc-cat-1")

	// Second run must not touch the already-active mapping.
	if err := Bind(context.Background(), params); err != nil {
		t.Fatalf("second Bind: %v", err)
	}
	second, _ := mappings.Get("category", "dc-cat-1")

	if first != second {
		t.Fatalf("second Bind mutated an already-active mapping: %+v -> %+v", first, second)
	}
}

func TestBind_AmbiguousNameMatch_SkipsBothCandidates(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Categories: []stoat.CategoryInfo{
				{ID: "stoat-cat-1", Title: "General"},
				{ID: "stoat-cat-2", Title: "General"},
			},
		},
	}

	err := Bind(context.Background(), Params{
		ServerID:   "srv1",
		Categories: []canonical.Category{{ID: "dc-cat-1", Name: "General"}},
		Mappings:   mappings,
		Reader:     reader,
		DryRun:     true,
		Logger:     testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	m, _ := mappings.Get("category", "dc-cat-1")
	if m.Found {
		t.Fatalf("mapping = %+v, want unbound (ambiguous match must never guess)", m)
	}
}

func TestBind_ChannelTypeMismatch_DoesNotMatch(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{ChannelIDs: []string{"stoat-chan-1"}},
		channels: map[string]stoat.ChannelInfo{
			"stoat-chan-1": {ID: "stoat-chan-1", Name: "general", Type: canonical.ChannelTypeVoice},
		},
	}

	err := Bind(context.Background(), Params{
		ServerID: "srv1",
		Channels: []canonical.Channel{{ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText}},
		Mappings: mappings,
		Reader:   reader,
		DryRun:   true,
		Logger:   testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	m, _ := mappings.Get("channel", "dc-chan-1")
	if m.Found {
		t.Fatalf("mapping = %+v, want unbound (same name, different type must not match)", m)
	}
}

func TestBind_ForeignStoatEntity_DryRun_NeverDeleted(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Categories: []stoat.CategoryInfo{{ID: "stoat-cat-native", Title: "NativeOnly"}},
		},
	}

	err := Bind(context.Background(), Params{
		ServerID: "srv1",
		Mappings: mappings,
		Reader:   reader,
		DryRun:   true,
		Logger:   testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	// No mapping should exist for the foreign category -- it was only
	// logged, never deleted or bound to anything.
	if len(mappings.rows) != 0 {
		t.Fatalf("mappings = %+v, want empty (foreign entity must not be touched)", mappings.rows)
	}
}

// Foreign categories have no delete-by-id path (StoatWriter's doc comment)
// -- Params.DryRun=false must not change that, or bindEntities panics
// dereferencing a nil Writer for a category.
func TestBind_ForeignCategory_AlwaysDryRun_EvenWithDryRunFalse(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Categories: []stoat.CategoryInfo{{ID: "stoat-cat-native", Title: "NativeOnly"}},
		},
	}

	err := Bind(context.Background(), Params{
		ServerID: "srv1",
		Mappings: mappings,
		Reader:   reader,
		DryRun:   false,
		Logger:   testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if len(mappings.rows) != 0 {
		t.Fatalf("mappings = %+v, want empty (foreign category must not be touched)", mappings.rows)
	}
}

// With Params.DryRun=false, a foreign channel/role (no Discord match, no
// mapping) must actually be deleted via StoatWriter, not just logged.
func TestBind_ForeignChannelAndRole_DryRunFalse_DeletesThroughWriter(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			ChannelIDs: []string{"stoat-chan-native"},
			Roles:      []stoat.RoleInfo{{ID: "stoat-role-native", Name: "NativeRole"}},
		},
		channels: map[string]stoat.ChannelInfo{
			"stoat-chan-native": {ID: "stoat-chan-native", Name: "native-channel", Type: canonical.ChannelTypeText},
		},
	}
	writer := &fakeWriter{}

	err := Bind(context.Background(), Params{
		ServerID: "srv1",
		Mappings: mappings,
		Reader:   reader,
		Writer:   writer,
		DryRun:   false,
		Logger:   testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if len(writer.deletedChannels) != 1 || writer.deletedChannels[0] != "stoat-chan-native" {
		t.Fatalf("deletedChannels = %v, want [stoat-chan-native]", writer.deletedChannels)
	}
	if len(writer.deletedRoles) != 1 || writer.deletedRoles[0] != "stoat-role-native" {
		t.Fatalf("deletedRoles = %v, want [stoat-role-native]", writer.deletedRoles)
	}
}

// A delete failure for one foreign entity must not abort the rest of Bind.
func TestBind_ForeignEntity_DeleteFails_LoggedNotFatal(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Roles: []stoat.RoleInfo{{ID: "stoat-role-native", Name: "NativeRole"}},
		},
	}
	writer := &fakeWriter{deleteErr: errors.New("stoat: 500")}

	err := Bind(context.Background(), Params{
		ServerID: "srv1",
		Mappings: mappings,
		Reader:   reader,
		Writer:   writer,
		DryRun:   false,
		Logger:   testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v, want no error even when the delete call itself fails", err)
	}
}

// Live-reproduced (see conversation notes on the "4 copies of roles" bug):
// the bot's own elevation role has no Discord counterpart, so it's
// unclaimed at the end of the role bind pass just like any other foreign
// role -- but it must never be logged/treated as reapable, or the bot loses
// the permission rank it needs to write anything at all.
func TestBind_BotElevationRole_ExemptFromForeignReap(t *testing.T) {
	mappings := newFakeMappings()
	var logBuf bytes.Buffer
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Roles: []stoat.RoleInfo{
				{ID: "stoat-role-bot", Name: "Stoatcord", Rank: 0},
				{ID: "stoat-role-native", Name: "SomeOtherNativeRole", Rank: 1},
			},
		},
		selfRoleIDs: []string{"stoat-role-bot"},
	}

	err := Bind(context.Background(), Params{
		ServerID: "srv1",
		Mappings: mappings,
		Reader:   reader,
		DryRun:   true,
		Logger:   slog.New(slog.NewTextHandler(&logBuf, nil)),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	for _, line := range strings.Split(logBuf.String(), "\n") {
		if strings.Contains(line, "stoat_id=stoat-role-bot") && strings.Contains(line, "would delete") {
			t.Fatalf("bot's own elevation role was logged as foreign/reapable:\n%s", line)
		}
	}
	logs := logBuf.String()
	if !strings.Contains(logs, "exempt from foreign-entity reap") {
		t.Fatalf("expected an exemption log line for the bot's elevation role, got:\n%s", logs)
	}
	if !strings.Contains(logs, `stoat_id=stoat-role-native`) || !strings.Contains(logs, "would delete") {
		t.Fatalf("expected the genuinely foreign role to still be flagged, got:\n%s", logs)
	}
}

// Live-reproduced: a channel the bot cannot view (role-restricted, no
// ViewChannel grant) 403s on FetchChannel. That must not abort the whole
// bind pass -- the channel is simply excluded from identity matching.
func TestBind_ChannelFetchError_SkipsChannelWithoutAbortingBind(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Categories: []stoat.CategoryInfo{{ID: "stoat-cat-1", Title: "General"}},
			ChannelIDs: []string{"stoat-chan-forbidden", "stoat-chan-1"},
		},
		channels: map[string]stoat.ChannelInfo{
			"stoat-chan-1": {ID: "stoat-chan-1", Name: "general", Type: canonical.ChannelTypeText},
		},
		channelErrs: map[string]error{
			"stoat-chan-forbidden": errors.New("403: MissingPermission ViewChannel"),
		},
	}

	err := Bind(context.Background(), Params{
		ServerID:   "srv1",
		Categories: []canonical.Category{{ID: "dc-cat-1", Name: "General"}},
		Channels:   []canonical.Channel{{ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText}},
		Mappings:   mappings,
		Reader:     reader,
		DryRun:     true,
		Logger:     testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v, want no error despite one channel's fetch failing", err)
	}

	catMapping, _ := mappings.Get("category", "dc-cat-1")
	if !catMapping.Found {
		t.Fatalf("category mapping = %+v, want bound (unrelated to the channel fetch failure)", catMapping)
	}
	chanMapping, _ := mappings.Get("channel", "dc-chan-1")
	if !chanMapping.Found || chanMapping.StoatID != "stoat-chan-1" {
		t.Fatalf("channel mapping = %+v, want bound to stoat-chan-1 despite the other channel's fetch failure", chanMapping)
	}
}

// Reproduces a real session finding: a channel's mapping row was left
// active pointing at a Stoat channel id that no longer exists live (the
// underlying entity vanished without our knowledge). Because bindEntities
// treated "active mapping" as claimed regardless of whether the mapped
// Stoat id still exists, the real live channel of the same name was never
// re-adopted -- it sat there logged as "foreign, would delete" forever,
// and the stale mapping was never corrected either (ReconcileLive
// explicitly treats a live-fetch miss as out of scope, by design). Bind
// must detect this and clear the dead mapping so the same pass's
// name-matching re-adopts the live entity.
func TestBind_DeadMapping_ClearedAndReboundToLiveEntityOfSameName(t *testing.T) {
	mappings := newFakeMappings()
	mappings.WritePending("channel", "dc-chan-1", "{}")
	mappings.Confirm("channel", "dc-chan-1", "stoat-chan-dead") // stale: doesn't exist live

	reader := &fakeReader{
		server: stoat.ServerInfo{
			ChannelIDs: []string{"stoat-chan-live"}, // the dead id is NOT here
		},
		channels: map[string]stoat.ChannelInfo{
			"stoat-chan-live": {ID: "stoat-chan-live", Name: "general", Type: canonical.ChannelTypeText},
		},
	}

	err := Bind(context.Background(), Params{
		ServerID: "srv1",
		Channels: []canonical.Channel{{ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText}},
		Mappings: mappings,
		Reader:   reader,
		DryRun:   true,
		Logger:   testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	m, _ := mappings.Get("channel", "dc-chan-1")
	if !m.Found || m.Status != engine.StatusActive || m.StoatID != "stoat-chan-live" {
		t.Fatalf("channel mapping = %+v, want rebound to the live channel stoat-chan-live", m)
	}
}

func TestBind_NoStoatMatch_LeavesUnboundForConvergeToCreate(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{}

	err := Bind(context.Background(), Params{
		ServerID:   "srv1",
		Categories: []canonical.Category{{ID: "dc-cat-1", Name: "BrandNew"}},
		Mappings:   mappings,
		Reader:     reader,
		DryRun:     true,
		Logger:     testLogger(),
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}

	m, _ := mappings.Get("category", "dc-cat-1")
	if m.Found {
		t.Fatalf("mapping = %+v, want unbound so the converge pass creates it", m)
	}
}
