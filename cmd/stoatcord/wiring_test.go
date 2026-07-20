package main

import (
	"testing"

	"github.com/luar/stoatcord/internal/engine"
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
