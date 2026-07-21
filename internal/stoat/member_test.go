package stoat

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

func TestAddSelfToRole_AddsRoleWhileKeepingExisting(t *testing.T) {
	var gotBody []byte
	client, _ := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
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
			body, _ := jsonBody(r)
			mu.Lock()
			gotBody = body
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
	})

	if err := client.AddSelfToRole(context.Background(), "srv1", "admin1"); err != nil {
		t.Fatalf("AddSelfToRole: %v", err)
	}

	var decoded struct {
		Roles []string `json:"roles"`
	}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("unmarshal edit body: %v", err)
	}
	want := map[string]bool{"elevation": true, "admin1": true}
	if len(decoded.Roles) != 2 {
		t.Fatalf("roles = %v, want 2 entries", decoded.Roles)
	}
	for _, r := range decoded.Roles {
		if !want[r] {
			t.Errorf("unexpected role %q in edit body", r)
		}
	}
}

func TestAddSelfToRole_AlreadyHeld_NoOp(t *testing.T) {
	client, requests := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/users/@me", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"bot1"}`))
		})
		mux.HandleFunc("/servers/srv1/members/bot1", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			*reqs = append(*reqs, recordedRequest{r.Method, r.URL.Path, nil})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"roles":["elevation","admin1"]}`))
		})
	})

	if err := client.AddSelfToRole(context.Background(), "srv1", "admin1"); err != nil {
		t.Fatalf("AddSelfToRole: %v", err)
	}

	if len(*requests) != 1 {
		t.Fatalf("got %d requests, want 1 (fetch only, no edit): %+v", len(*requests), *requests)
	}
}
