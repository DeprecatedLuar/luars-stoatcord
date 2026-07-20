package stoat

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
)

func TestCreateRole_CreatesThenAppliesFieldsAndPermissions(t *testing.T) {
	var mu sync.Mutex
	var requests []recordedRequest

	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1/roles", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			requests = append(requests, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"role1"}`))
		})
		mux.HandleFunc("/servers/srv1/roles/role1", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			requests = append(requests, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
		mux.HandleFunc("/servers/srv1/permissions/role1", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			requests = append(requests, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
	})

	role := canonical.StoatRole{
		Name:   "Moderator",
		Colour: "#FF00AA",
		Hoist:  true,
		Rank:   5,
		Permissions: canonical.StoatOverwrite{
			Allow: 1,
			Deny:  2,
		},
	}

	id, err := client.CreateRole(context.Background(), "srv1", role)
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if id != "role1" {
		t.Fatalf("id = %q, want role1", id)
	}

	if len(requests) != 3 {
		t.Fatalf("got %d requests, want 3 (create + edit fields + set permissions): %+v", len(requests), requests)
	}
	if requests[0].method != http.MethodPost || requests[0].path != "/servers/srv1/roles" {
		t.Fatalf("create request = %+v", requests[0])
	}
	if requests[1].method != http.MethodPatch || requests[1].path != "/servers/srv1/roles/role1" {
		t.Fatalf("edit-fields request = %+v", requests[1])
	}
	var editBody struct {
		Colour string `json:"colour"`
		Hoist  bool   `json:"hoist"`
		Rank   int    `json:"rank"`
	}
	if err := json.Unmarshal(requests[1].body, &editBody); err != nil {
		t.Fatalf("unmarshal edit body: %v", err)
	}
	if editBody.Colour != "#FF00AA" || !editBody.Hoist || editBody.Rank != 5 {
		t.Fatalf("edit body = %+v", editBody)
	}
	if requests[2].method != http.MethodPut || requests[2].path != "/servers/srv1/permissions/role1" {
		t.Fatalf("permissions request = %+v", requests[2])
	}
}

func TestEditRole_UpdatesFieldsAndPermissions(t *testing.T) {
	var mu sync.Mutex
	var requests []recordedRequest

	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1/roles/role1", func(w http.ResponseWriter, r *http.Request) {
			body, _ := jsonBody(r)
			mu.Lock()
			requests = append(requests, recordedRequest{r.Method, r.URL.Path, body})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
		mux.HandleFunc("/servers/srv1/permissions/role1", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests = append(requests, recordedRequest{r.Method, r.URL.Path, nil})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
	})

	role := canonical.StoatRole{Name: "Renamed", Colour: "#000000", Hoist: false, Rank: 1}
	if err := client.EditRole(context.Background(), "srv1", "role1", role); err != nil {
		t.Fatalf("EditRole: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("got %d requests, want 2: %+v", len(requests), requests)
	}
	if requests[0].method != http.MethodPatch {
		t.Fatalf("edit method = %q, want PATCH", requests[0].method)
	}
	if requests[1].method != http.MethodPut {
		t.Fatalf("permissions method = %q, want PUT", requests[1].method)
	}
}

func TestDeleteRole_SendsDelete(t *testing.T) {
	var mu sync.Mutex
	var requests []recordedRequest

	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1/roles/role1", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			requests = append(requests, recordedRequest{r.Method, r.URL.Path, nil})
			mu.Unlock()
			w.Write([]byte(`{}`))
		})
	})

	if err := client.DeleteRole(context.Background(), "srv1", "role1"); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if len(requests) != 1 || requests[0].method != http.MethodDelete {
		t.Fatalf("requests = %+v, want single DELETE", requests)
	}
}
