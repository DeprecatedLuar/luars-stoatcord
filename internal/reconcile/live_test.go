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

func testLoggerBuf() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestReconcileLive_MatchingEntity_TrustsCacheAndStaysActive(t *testing.T) {
	mappings := newFakeMappings()
	mappings.WritePending("category", "dc-cat-1", "{}")
	mappings.Confirm("category", "dc-cat-1", "dc-cat-1")
	mappings.WritePending("channel", "dc-chan-1", "{}")
	mappings.Confirm("channel", "dc-chan-1", "stoat-chan-1")

	desiredCat := canonical.Category{ID: "dc-cat-1", Name: "General", ChannelIDs: []string{"dc-chan-1"}}

	logger, _ := testLoggerBuf()
	reader := &fakeReader{
		server: stoat.ServerInfo{
			Categories: []stoat.CategoryInfo{{ID: "dc-cat-1", Title: "General", ChannelIDs: []string{"stoat-chan-1"}}},
		},
		channels: map[string]stoat.ChannelInfo{
			"stoat-chan-1": {ID: "stoat-chan-1", Name: "general", Type: canonical.ChannelTypeText},
		},
	}

	err := ReconcileLive(context.Background(), Params{
		ServerID:   "srv1",
		GuildID:    "guild1",
		Categories: []canonical.Category{desiredCat},
		Channels:   []canonical.Channel{{ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText}},
		Mappings:   mappings,
		Reader:     reader,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("ReconcileLive: %v", err)
	}

	m, _ := mappings.Get("category", "dc-cat-1")
	if !m.Found || m.Status != engine.StatusActive {
		t.Fatalf("category mapping = %+v, want active", m)
	}
	wantJSON, _ := desiredCat.CanonicalJSON()
	if m.CanonicalState != string(wantJSON) {
		t.Fatalf("CanonicalState = %q, want desired's own CanonicalJSON %q", m.CanonicalState, wantJSON)
	}
}

func TestReconcileLive_DriftedChannel_ForcesEmptyStateAndLogs(t *testing.T) {
	mappings := newFakeMappings()
	mappings.WritePending("channel", "dc-chan-1", "{}")
	mappings.Confirm("channel", "dc-chan-1", "stoat-chan-1")

	logger, buf := testLoggerBuf()
	reader := &fakeReader{
		server: stoat.ServerInfo{},
		channels: map[string]stoat.ChannelInfo{
			// Live channel name differs from desired -- drift.
			"stoat-chan-1": {ID: "stoat-chan-1", Name: "renamed-live", Type: canonical.ChannelTypeText},
		},
	}

	err := ReconcileLive(context.Background(), Params{
		ServerID: "srv1",
		GuildID:  "guild1",
		Channels: []canonical.Channel{{ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText}},
		Mappings: mappings,
		Reader:   reader,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("ReconcileLive: %v", err)
	}

	m, _ := mappings.Get("channel", "dc-chan-1")
	if !m.Found || m.Status != engine.StatusActive {
		t.Fatalf("channel mapping = %+v, want active", m)
	}
	if m.CanonicalState != emptyCanonicalState {
		t.Fatalf("CanonicalState = %q, want %q (forces the next Diff to push a real correction)", m.CanonicalState, emptyCanonicalState)
	}
	if !strings.Contains(buf.String(), "dc-chan-1") {
		t.Fatalf("log output = %q, want it to name the drifted channel's discord id", buf.String())
	}
}

func TestReconcileLive_UnmappedEntity_Untouched(t *testing.T) {
	mappings := newFakeMappings() // no mapping row for dc-chan-1 at all

	logger, _ := testLoggerBuf()
	reader := &fakeReader{server: stoat.ServerInfo{}}

	err := ReconcileLive(context.Background(), Params{
		ServerID: "srv1",
		GuildID:  "guild1",
		Channels: []canonical.Channel{{ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText}},
		Mappings: mappings,
		Reader:   reader,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("ReconcileLive: %v", err)
	}

	m, _ := mappings.Get("channel", "dc-chan-1")
	if m.Found {
		t.Fatalf("mapping = %+v, want untouched/unbound (ConvergeAll's create path owns this entity)", m)
	}
}

func TestReconcileLive_RoleAndDefaultPermissionReverseMapping(t *testing.T) {
	mappings := newFakeMappings()
	mappings.WritePending("role", "dc-role-1", "{}")
	mappings.Confirm("role", "dc-role-1", "stoat-role-1")
	mappings.WritePending("channel", "dc-chan-1", "{}")
	mappings.Confirm("channel", "dc-chan-1", "stoat-chan-1")

	desiredCh := canonical.Channel{
		ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText,
		Overwrites: map[string]canonical.Overwrite{
			// "guild1" is the discord @everyone role id (== GuildID) --
			// must reverse-map against live DefaultPermissions, not
			// RolePermissions["default"].
			"guild1":    {Deny: []canonical.Permission{canonical.PermViewChannel}},
			"dc-role-1": {Allow: []canonical.Permission{canonical.PermViewChannel}},
		},
	}

	logger, _ := testLoggerBuf()
	reader := &fakeReader{
		server: stoat.ServerInfo{},
		channels: map[string]stoat.ChannelInfo{
			"stoat-chan-1": {
				ID: "stoat-chan-1", Name: "general", Type: canonical.ChannelTypeText,
				DefaultPermissions: &canonical.StoatOverwrite{Deny: canonical.StoatBits([]canonical.Permission{canonical.PermViewChannel}, logger)},
				RolePermissions: map[string]canonical.StoatOverwrite{
					"stoat-role-1": {Allow: canonical.StoatBits([]canonical.Permission{canonical.PermViewChannel}, logger)},
				},
			},
		},
	}

	err := ReconcileLive(context.Background(), Params{
		ServerID: "srv1",
		GuildID:  "guild1",
		Roles:    []canonical.Role{{ID: "dc-role-1", Name: "SomeRole"}},
		Channels: []canonical.Channel{desiredCh},
		Mappings: mappings,
		Reader:   reader,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("ReconcileLive: %v", err)
	}

	m, _ := mappings.Get("channel", "dc-chan-1")
	wantJSON, _ := desiredCh.CanonicalJSON()
	if m.CanonicalState != string(wantJSON) {
		t.Fatalf("CanonicalState = %q, want the desired (matching) state %q -- reverse mapping of default/role overwrites must have found a match", m.CanonicalState, wantJSON)
	}
}

func TestReconcileLive_Fetch404_LeavesMappingUntouchedAndLogsWarn(t *testing.T) {
	mappings := newFakeMappings()
	mappings.WritePending("channel", "dc-chan-1", `{"stale":"state"}`)
	mappings.Confirm("channel", "dc-chan-1", "stoat-chan-1")
	before, _ := mappings.Get("channel", "dc-chan-1")

	logger, buf := testLoggerBuf()
	reader := &fakeReader{
		server:      stoat.ServerInfo{},
		channelErrs: map[string]error{"stoat-chan-1": errors.New("404: NotFound")},
	}

	err := ReconcileLive(context.Background(), Params{
		ServerID: "srv1",
		GuildID:  "guild1",
		Channels: []canonical.Channel{{ID: "dc-chan-1", Name: "general", Type: canonical.ChannelTypeText}},
		Mappings: mappings,
		Reader:   reader,
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("ReconcileLive: %v", err)
	}

	after, _ := mappings.Get("channel", "dc-chan-1")
	if before != after {
		t.Fatalf("mapping mutated on fetch failure: %+v -> %+v, want untouched", before, after)
	}
	if !strings.Contains(buf.String(), "dc-chan-1") {
		t.Fatalf("log output = %q, want it to name the channel whose fetch failed", buf.String())
	}
}
