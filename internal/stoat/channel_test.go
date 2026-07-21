package stoat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
)

type recordedRequest struct {
	method string
	path   string
	body   []byte
}

func newTestServer(t *testing.T, extra func(mux *http.ServeMux, mu *sync.Mutex, requests *[]recordedRequest)) (*Client, *[]recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []recordedRequest

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revolt":"0.14.2","ws":"wss://events.stoat.chat"}`))
	})
	if extra != nil {
		extra(mux, &mu, &requests)
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := New("test-token", srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, &requests
}

func TestCreateChannel_TextTypeAndAppliesOverwrites(t *testing.T) {
	client, requests := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1/channels", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"chan1","name":"general"}`))
		})
		mux.HandleFunc("/channels/chan1/permissions/role1", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
	})

	ch := canonical.StoatChannel{
		Name: "general",
		Type: "Text",
		Overwrites: map[string]canonical.StoatOverwrite{
			"role1": {Allow: 5, Deny: 2},
		},
	}

	id, err := client.CreateChannel(context.Background(), "srv1", ch)
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if id != "chan1" {
		t.Fatalf("id = %q, want chan1", id)
	}

	if len(*requests) != 2 {
		t.Fatalf("got %d requests, want 2 (create + 1 overwrite): %+v", len(*requests), *requests)
	}
	if (*requests)[0].method != http.MethodPost || (*requests)[0].path != "/servers/srv1/channels" {
		t.Fatalf("create request = %+v", (*requests)[0])
	}
	if (*requests)[1].path != "/channels/chan1/permissions/role1" {
		t.Fatalf("permission request = %+v", (*requests)[1])
	}
}

// The bot's own rank-0 elevation role can never successfully self-grant a
// channel override (Stoat's permissions_set.rs refuses a non-owner writing
// an override for a role at or above their own rank, and the elevation
// role's rank IS the bot's own rank) -- so CreateChannel must never target
// it at all.
func TestCreateChannel_DoesNotInjectElevationRoleOverride(t *testing.T) {
	client, requests := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{"elevation": {"name": "Stoatcord", "rank": 0}}`, `["elevation"]`)
		mux.HandleFunc("/servers/srv1/channels", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, nil})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"chan1","name":"general"}`))
		})
		mux.HandleFunc("/channels/chan1/permissions/elevation", func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("CreateChannel must not write a permission override for the elevation role")
		})
	})
	if err := client.ResolveElevationRole(context.Background(), "srv1"); err != nil {
		t.Fatalf("ResolveElevationRole: %v", err)
	}

	if _, err := client.CreateChannel(context.Background(), "srv1", canonical.StoatChannel{Name: "general", Type: "Text"}); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if len(*requests) != 1 {
		t.Fatalf("got %d requests, want 1 (create only, no overwrites): %+v", len(*requests), *requests)
	}
}

// Admin-mapped roles (Discord roles that carried ADMINISTRATOR) are injected
// on every channel, unconditionally, first -- before any Discord-derived
// overwrite -- since the bot (wearing rank 0) can write overrides for any
// role ranked worse than itself, unlike its own elevation role.
func TestCreateChannel_InjectsAdminRoleSelfGrantsFirst(t *testing.T) {
	const elevationPermissions = 1099510595551
	client, requests := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		serverAndMemberHandlers(mux, `{
			"elevation": {"name": "Stoatcord", "rank": 0, "permissions": {"a": `+fmt.Sprint(elevationPermissions)+`, "d": 0}}
		}`, `["elevation"]`)
		mux.HandleFunc("/servers/srv1/channels", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, nil})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"chan1","name":"general"}`))
		})
		mux.HandleFunc("/channels/chan1/permissions/admin1", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
		mux.HandleFunc("/channels/chan1/permissions/admin2", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
		mux.HandleFunc("/channels/chan1/permissions/role1", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, nil})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
	})
	client.SetAdminRoleIDs([]string{"admin1", "admin2"})
	if err := client.ResolveElevationRole(context.Background(), "srv1"); err != nil {
		t.Fatalf("ResolveElevationRole: %v", err)
	}

	ch := canonical.StoatChannel{
		Name: "general",
		Type: "Text",
		Overwrites: map[string]canonical.StoatOverwrite{
			"role1": {Allow: 5, Deny: 2},
		},
	}
	if _, err := client.CreateChannel(context.Background(), "srv1", ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if len(*requests) != 4 {
		t.Fatalf("got %d requests, want 4 (create + 2 admin self-grants + role1 overwrite): %+v", len(*requests), *requests)
	}
	if (*requests)[1].path != "/channels/chan1/permissions/admin1" || (*requests)[2].path != "/channels/chan1/permissions/admin2" {
		t.Fatalf("admin self-grants = %+v, %+v, want admin1 then admin2 before any Discord-derived overwrite", (*requests)[1], (*requests)[2])
	}
	var grantBody struct {
		Permissions struct {
			Allow uint64 `json:"allow"`
			Deny  uint64 `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal((*requests)[1].body, &grantBody); err != nil {
		t.Fatalf("unmarshal self-grant body: %v", err)
	}
	if grantBody.Permissions.Allow != elevationPermissions || grantBody.Permissions.Deny != 0 {
		t.Fatalf("self-grant = %+v, want allow=%d deny=0", grantBody.Permissions, elevationPermissions)
	}
	if (*requests)[3].path != "/channels/chan1/permissions/role1" {
		t.Fatalf("fourth request = %+v, want role1's overwrite", (*requests)[3])
	}
}

func TestCreateChannel_AdminRolesWithoutResolvedElevation_Fails(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1/channels", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"chan1","name":"general"}`))
		})
		mux.HandleFunc("/channels/chan1/permissions/admin1", func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("must not attempt a self-grant before the elevation role is resolved")
		})
	})
	client.SetAdminRoleIDs([]string{"admin1"})

	_, err := client.CreateChannel(context.Background(), "srv1", canonical.StoatChannel{Name: "general", Type: "Text"})
	if err == nil {
		t.Fatal("CreateChannel: want error, elevation role was never resolved")
	}
}

func TestCreateChannel_VoiceType(t *testing.T) {
	var gotBody []byte
	client, _ := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1/channels", func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = jsonBody(r)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"chan1","name":"voice-general"}`))
		})
	})

	_, err := client.CreateChannel(context.Background(), "srv1", canonical.StoatChannel{Name: "voice-general", Type: "Voice"})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	var decoded struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != "Voice" {
		t.Fatalf("type = %q, want Voice", decoded.Type)
	}
}

func TestEditChannel_UpdatesNameAndOverwrites(t *testing.T) {
	client, requests := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/channels/chan1", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
		mux.HandleFunc("/channels/chan1/permissions/role1", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
	})

	ch := canonical.StoatChannel{
		Name: "renamed",
		Type: "Text",
		Overwrites: map[string]canonical.StoatOverwrite{
			"role1": {Allow: 1, Deny: 0},
		},
	}
	if err := client.EditChannel(context.Background(), "chan1", ch); err != nil {
		t.Fatalf("EditChannel: %v", err)
	}

	if len(*requests) != 2 {
		t.Fatalf("got %d requests, want 2: %+v", len(*requests), *requests)
	}
	if (*requests)[0].method != http.MethodPatch {
		t.Fatalf("edit method = %q, want PATCH", (*requests)[0].method)
	}
}

func TestDeleteChannel_SendsDelete(t *testing.T) {
	client, requests := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/channels/chan1", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, nil})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
	})

	if err := client.DeleteChannel(context.Background(), "chan1"); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if len(*requests) != 1 || (*requests)[0].method != http.MethodDelete {
		t.Fatalf("requests = %+v, want single DELETE", *requests)
	}
}

func jsonBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
