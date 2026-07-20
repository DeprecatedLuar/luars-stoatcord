package reconcile

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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

func TestBind_ForeignStoatEntity_NeverDeleted(t *testing.T) {
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

func TestBind_NoStoatMatch_LeavesUnboundForConvergeToCreate(t *testing.T) {
	mappings := newFakeMappings()
	reader := &fakeReader{}

	err := Bind(context.Background(), Params{
		ServerID:   "srv1",
		Categories: []canonical.Category{{ID: "dc-cat-1", Name: "BrandNew"}},
		Mappings:   mappings,
		Reader:     reader,
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
