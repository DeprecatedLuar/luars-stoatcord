package stoat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetChannelPermissions_SendsTriStateBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []byte

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revolt":"0.14.2","ws":"wss://events.stoat.chat"}`))
	})
	mux.HandleFunc("/channels/chan1/permissions/role1", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := New("test-token", srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := client.SetChannelPermissions(context.Background(), "chan1", "role1", 5, 2); err != nil {
		t.Fatalf("SetChannelPermissions: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/channels/chan1/permissions/role1" {
		t.Fatalf("path = %q", gotPath)
	}

	var body struct {
		Permissions struct {
			Allow uint64 `json:"allow"`
			Deny  uint64 `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("unmarshal body %s: %v", gotBody, err)
	}
	if body.Permissions.Allow != 5 || body.Permissions.Deny != 2 {
		t.Fatalf("body = %+v, want allow=5 deny=2 (tri-state shape, not the library's flat uint)", body)
	}
}

// ServerSetRolePermissions in the library sends {"permissions":{"server":X,
// "channel":Y}} -- the wrong shape. implementation-history.md confirmed live
// that server.roles[id].permissions is tri-state {a: allow, d: deny}, same
// as channel overwrites, so this bypasses the library helper the same way.
func TestSetRolePermissions_SendsTriStateBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []byte

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revolt":"0.14.2","ws":"wss://events.stoat.chat"}`))
	})
	mux.HandleFunc("/servers/srv1/permissions/role1", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := New("test-token", srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := client.SetRolePermissions(context.Background(), "srv1", "role1", 8, 4); err != nil {
		t.Fatalf("SetRolePermissions: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/servers/srv1/permissions/role1" {
		t.Fatalf("path = %q", gotPath)
	}

	var body struct {
		Permissions struct {
			Allow uint64 `json:"allow"`
			Deny  uint64 `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("unmarshal body %s: %v", gotBody, err)
	}
	if body.Permissions.Allow != 8 || body.Permissions.Deny != 4 {
		t.Fatalf("body = %+v, want allow=8 deny=4", body)
	}
}
