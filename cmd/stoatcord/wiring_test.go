package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/stoat"
	"github.com/luar/stoatcord/internal/store"
)

// mappingStoreAdapter must satisfy engine.MappingStore so *store.Store can
// be wired straight into engine.New (Phase 4 wiring gap).
var _ engine.MappingStore = mappingStoreAdapter{}

func TestMappingStoreAdapter_GetTranslatesFoundRow(t *testing.T) {
	st := openTestStore(t)
	adapter := mappingStoreAdapter{st}

	if err := st.WritePending("channel", "d1", `{"name":"general"}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := st.Confirm("channel", "d1", "s1"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	got, err := adapter.Get("channel", "d1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	want := engine.Mapping{
		Found:          true,
		StoatID:        "s1",
		Status:         engine.StatusActive,
		CanonicalState: `{"name":"general"}`,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMappingStoreAdapter_GetTranslatesNotFound(t *testing.T) {
	st := openTestStore(t)
	adapter := mappingStoreAdapter{st}

	got, err := adapter.Get("channel", "missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Found {
		t.Fatalf("got Found=true for missing row")
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newTestStoatClient(t *testing.T, extra func(mux *http.ServeMux)) *stoat.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revolt":"0.14.2","ws":"wss://events.stoat.chat"}`))
	})
	if extra != nil {
		extra(mux)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := stoat.New("test-token", srv.URL)
	if err != nil {
		t.Fatalf("stoat.New: %v", err)
	}
	return client
}

func TestResolveAdminRoles_NoDiscordAdministratorRole_Errors(t *testing.T) {
	st := openTestStore(t)
	client := newTestStoatClient(t, nil)

	roles := []canonical.Role{{ID: "d1", Name: "Member", Rank: 1}}

	if _, err := resolveAdminRoles(context.Background(), st, client, "srv1", roles, nil); err == nil {
		t.Fatal("resolveAdminRoles: want error, no role carries ADMINISTRATOR")
	}
}

func TestResolveAdminRoles_MirrorsMissingRoleThenSelfAssigns(t *testing.T) {
	st := openTestStore(t)
	client := newTestStoatClient(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/servers/srv1/roles", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"s-admin"}`))
		})
		mux.HandleFunc("/servers/srv1/roles/s-admin", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{}`))
		})
		mux.HandleFunc("/servers/srv1/permissions/s-admin", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{}`))
		})
		mux.HandleFunc("/users/@me", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"bot1"}`))
		})
		mux.HandleFunc("/servers/srv1/members/bot1", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"roles":["elevation"]}`))
				return
			}
			w.Write([]byte(`{}`))
		})
	})

	roles := []canonical.Role{{ID: "d-admin", Name: "Admin", Rank: 3, Privileged: true}}

	got, err := resolveAdminRoles(context.Background(), st, client, "srv1", roles, nil)
	if err != nil {
		t.Fatalf("resolveAdminRoles: %v", err)
	}
	if len(got) != 1 || got[0] != "s-admin" {
		t.Fatalf("got %v, want [s-admin]", got)
	}

	mapping, err := st.Get("role", "d-admin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !mapping.Found || mapping.Status != "active" || mapping.StoatID != "s-admin" {
		t.Fatalf("mapping = %+v, want an active row for s-admin", mapping)
	}

	if got := client.AdminRoleIDs(); len(got) != 1 || got[0] != "s-admin" {
		t.Fatalf("client.AdminRoleIDs() = %v, want [s-admin]", got)
	}
}

func TestResolveAdminRoles_AlreadyMirroredAndHeld_SkipsCreateAndSelfAssign(t *testing.T) {
	st := openTestStore(t)
	if err := st.WritePending("role", "d-admin", `{}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := st.Confirm("role", "d-admin", "s-admin"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	client := newTestStoatClient(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/servers/srv1/roles", func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("must not create a role that's already mirrored")
		})
		mux.HandleFunc("/users/@me", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"bot1"}`))
		})
		mux.HandleFunc("/servers/srv1/members/bot1", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Fatal("must not self-assign a role the bot already holds")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"roles":["elevation","s-admin"]}`))
		})
	})

	roles := []canonical.Role{{ID: "d-admin", Name: "Admin", Rank: 3, Privileged: true}}

	got, err := resolveAdminRoles(context.Background(), st, client, "srv1", roles, nil)
	if err != nil {
		t.Fatalf("resolveAdminRoles: %v", err)
	}
	if len(got) != 1 || got[0] != "s-admin" {
		t.Fatalf("got %v, want [s-admin]", got)
	}
}
