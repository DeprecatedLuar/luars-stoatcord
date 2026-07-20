package stoat

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew_OverridesWSURLFromFetchedSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revolt":"0.14.2","ws":"wss://events.stoat.chat","app":"stoat.chat"}`))
	}))
	defer srv.Close()

	client, err := New("test-token", srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := client.WSURL(); got != "wss://events.stoat.chat" {
		t.Fatalf("WSURL = %q, want %q (library's hardcoded default must be overridden from Settings.Ws)", got, "wss://events.stoat.chat")
	}
}
