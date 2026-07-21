package stoat

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
)

// serverAndMemberHandlers wires the two endpoints ResolveElevationRole
// needs: the server's roles (with ranks) and the bot's own member entry
// (which roles it wears). selfRoleIDs is a JSON array literal, e.g.
// `["role1"]`.
func serverAndMemberHandlers(mux *http.ServeMux, rolesJSON, selfRoleIDsJSON string) {
	mux.HandleFunc("/servers/srv1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"S","channels":[],"categories":[],"roles":` + rolesJSON + `}`))
	})
	mux.HandleFunc("/users/@me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"_id":"bot1"}`))
	})
	mux.HandleFunc("/servers/srv1/members/bot1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"roles":` + selfRoleIDsJSON + `}`))
	})
}

func TestResolveElevationRole_BotWearsRankZero_Succeeds(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{
			"elevation": {"name": "Stoatcord", "rank": 0, "permissions": {"a": 1099510595551, "d": 0}},
			"other": {"name": "Member", "rank": 5}
		}`, `["elevation"]`)
	})

	if got := client.ElevationPermissions(); got != 0 {
		t.Fatalf("ElevationPermissions() before resolve = %d, want 0", got)
	}

	if err := client.ResolveElevationRole(context.Background(), "srv1"); err != nil {
		t.Fatalf("ResolveElevationRole: %v", err)
	}
	if got := client.ElevationRoleID(); got != "elevation" {
		t.Fatalf("ElevationRoleID() = %q, want elevation", got)
	}
	if got := client.ElevationPermissions(); got != 1099510595551 {
		t.Fatalf("ElevationPermissions() = %d, want 1099510595551", got)
	}
}

func TestResolveElevationRole_NoRoleAtRankZero_Fails(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{
			"mine": {"name": "Stoatcord", "rank": 2}
		}`, `["mine"]`)
	})

	if err := client.ResolveElevationRole(context.Background(), "srv1"); err == nil {
		t.Fatal("ResolveElevationRole: want error, no role on the server sits at rank 0")
	}
	if got := client.ElevationRoleID(); got != "" {
		t.Fatalf("ElevationRoleID() = %q, want empty after a failed resolve", got)
	}
	if got := client.ElevationPermissions(); got != 0 {
		t.Fatalf("ElevationPermissions() = %d, want 0 after a failed resolve", got)
	}
}

func TestResolveElevationRole_RankZeroRoleExistsButBotDoesNotWearIt_Fails(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{
			"native-top": {"name": "SomeoneElse", "rank": 0},
			"mine": {"name": "Stoatcord", "rank": 1}
		}`, `["mine"]`)
	})

	if err := client.ResolveElevationRole(context.Background(), "srv1"); err == nil {
		t.Fatal("ResolveElevationRole: want error, the bot doesn't wear the rank-0 role")
	}
}

func TestResolveElevationRole_MultipleRolesTiedAtRankZero_Fails(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{
			"mine": {"name": "Stoatcord", "rank": 0},
			"tied": {"name": "SomehowAlsoTop", "rank": 0}
		}`, `["mine"]`)
	})

	if err := client.ResolveElevationRole(context.Background(), "srv1"); err == nil {
		t.Fatal("ResolveElevationRole: want error, rank 0 is ambiguous between two roles")
	}
}

func TestResolveElevationRole_BotWearsNoRoles_Fails(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{
			"other": {"name": "Member", "rank": 0}
		}`, `[]`)
	})

	if err := client.ResolveElevationRole(context.Background(), "srv1"); err == nil {
		t.Fatal("ResolveElevationRole: want error, bot wears no roles at all")
	}
}

func TestEditRole_RefusesToWriteElevationRole(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{"elevation": {"name": "Stoatcord", "rank": 0}}`, `["elevation"]`)
		mux.HandleFunc("/servers/srv1/roles/elevation", func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("EditRole must not send a request for the elevation role")
		})
	})
	if err := client.ResolveElevationRole(context.Background(), "srv1"); err != nil {
		t.Fatalf("ResolveElevationRole: %v", err)
	}

	if err := client.EditRole(context.Background(), "srv1", "elevation", canonical.StoatRole{Name: "whatever"}); err == nil {
		t.Fatal("EditRole: want error, refusing to write the bot's own elevation role")
	}
}

func TestDeleteRole_RefusesToDeleteElevationRole(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{"elevation": {"name": "Stoatcord", "rank": 0}}`, `["elevation"]`)
		mux.HandleFunc("/servers/srv1/roles/elevation", func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("DeleteRole must not send a request for the elevation role")
		})
	})
	if err := client.ResolveElevationRole(context.Background(), "srv1"); err != nil {
		t.Fatalf("ResolveElevationRole: %v", err)
	}

	if err := client.DeleteRole(context.Background(), "srv1", "elevation"); err == nil {
		t.Fatal("DeleteRole: want error, refusing to delete the bot's own elevation role")
	}
}
