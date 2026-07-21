package stoat

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
)

func TestSendMessage_PostsContentAndMasquerade(t *testing.T) {
	var gotBody []byte
	client, _ := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/channels/chan1/messages", func(w http.ResponseWriter, r *http.Request) {
			gotBody, _ = jsonBody(r)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id":"stoat-msg1"}`))
		})
	})

	msg := canonical.StoatMessage{
		Content: "hello",
		Masquerade: canonical.StoatMasquerade{
			Name:   "Alice",
			Avatar: "https://cdn.example/a.png",
			Colour: "#FF00AA",
		},
	}

	id, err := client.SendMessage(context.Background(), "chan1", msg)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if id != "stoat-msg1" {
		t.Fatalf("id = %q, want stoat-msg1", id)
	}

	var decoded struct {
		Content    string `json:"content"`
		Masquerade struct {
			Name   string `json:"name"`
			Avatar string `json:"avatar"`
			Colour string `json:"colour"`
		} `json:"masquerade"`
	}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if decoded.Content != "hello" {
		t.Fatalf("content = %q, want hello", decoded.Content)
	}
	if decoded.Masquerade.Name != "Alice" || decoded.Masquerade.Avatar != "https://cdn.example/a.png" || decoded.Masquerade.Colour != "#FF00AA" {
		t.Fatalf("masquerade = %+v", decoded.Masquerade)
	}
}

func TestEditMessage_PatchesContentOnly(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []byte
	client, _ := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/channels/chan1/messages/msg1", func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotBody, _ = jsonBody(r)
			w.Write([]byte(`{}`))
		})
	})

	if err := client.EditMessage(context.Background(), "chan1", "msg1", "updated content"); err != nil {
		t.Fatalf("EditMessage: %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/channels/chan1/messages/msg1" {
		t.Fatalf("path = %q, want /channels/chan1/messages/msg1", gotPath)
	}

	var decoded struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if decoded.Content != "updated content" {
		t.Fatalf("content = %q, want %q", decoded.Content, "updated content")
	}
}

func TestDeleteMessage_SendsDelete(t *testing.T) {
	var gotMethod, gotPath string
	client, _ := newTestServer(t, func(mux *http.ServeMux, mu *sync.Mutex, reqs *[]recordedRequest) {
		mux.HandleFunc("/channels/chan1/messages/msg1", func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			w.Write([]byte(`{}`))
		})
	})

	if err := client.DeleteMessage(context.Background(), "chan1", "msg1"); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/channels/chan1/messages/msg1" {
		t.Fatalf("path = %q, want /channels/chan1/messages/msg1", gotPath)
	}
}
