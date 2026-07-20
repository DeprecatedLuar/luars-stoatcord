package stoat

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
)

func TestEditServer_SendsMetadata(t *testing.T) {
	var gotBody []byte
	client, _ := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1", func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = jsonBody(r)
			w.Write([]byte(`{}`))
		})
	})

	meta := canonical.StoatServer{Name: "My Server", Description: "desc"}
	if err := client.EditServer(context.Background(), "srv1", meta); err != nil {
		t.Fatalf("EditServer: %v", err)
	}

	var decoded struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Name != "My Server" || decoded.Description != "desc" {
		t.Fatalf("body = %+v", decoded)
	}
}

// SetCategories replaces the server's entire categories array in one PATCH,
// since Stoat's wire model has no per-category CRUD -- categories are a
// server-level ordered list (spec 6), never edited piecemeal.
func TestSetCategories_ReplacesWholeList(t *testing.T) {
	var gotBody []byte
	client, _ := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1", func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = jsonBody(r)
			w.Write([]byte(`{}`))
		})
	})

	cats := []canonical.Category{
		{ID: "cat1", Name: "General", ChannelIDs: []string{"c1", "c2"}},
		{ID: "cat2", Name: "Voice", ChannelIDs: []string{"c3"}},
	}
	if err := client.SetCategories(context.Background(), "srv1", cats); err != nil {
		t.Fatalf("SetCategories: %v", err)
	}

	var decoded struct {
		Categories []struct {
			ID       string   `json:"id"`
			Title    string   `json:"title"`
			Channels []string `json:"channels"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Categories) != 2 {
		t.Fatalf("categories = %+v, want 2", decoded.Categories)
	}
	if decoded.Categories[0].ID != "cat1" || decoded.Categories[0].Title != "General" || len(decoded.Categories[0].Channels) != 2 {
		t.Fatalf("categories[0] = %+v", decoded.Categories[0])
	}
	if decoded.Categories[1].Channels[0] != "c3" {
		t.Fatalf("categories[1] = %+v", decoded.Categories[1])
	}
}
