package stoat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The library gates every Request behind its own internal 3-second Ticker
// (gap 4, undocumented in implementation-plan.md's three known gaps). Our
// architecture's single intended throttle is engine.GlobalRateLimiter
// (spec: "single global throttle shared by all workers"), so this
// library-internal one must be neutralized in New, not left to double-gate
// every call on top of ours.
func TestNew_NeutralizesLibraryInternalTicker(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revolt":"0.14.2","ws":"wss://events.stoat.chat"}`))
	})
	mux.HandleFunc("/channels/chan1/permissions/role1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := New("test-token", srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	for range 3 {
		if err := client.SetChannelPermissions(context.Background(), "chan1", "role1", 1, 0); err != nil {
			t.Fatalf("SetChannelPermissions: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("3 requests took %v, want well under 500ms (library's internal Ticker not neutralized)", elapsed)
	}
}
