package store

import "testing"

func TestMappingStore_Get_NotFoundReturnsZeroValue(t *testing.T) {
	st := openTestDB(t)

	m, err := st.Get("channel", "chan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.Found {
		t.Fatalf("expected Found=false for a row that was never written, got %+v", m)
	}
}

func TestMappingStore_WritePending_ThenGet_ReturnsPendingRowWithNoStoatID(t *testing.T) {
	st := openTestDB(t)

	if err := st.WritePending("channel", "chan-1", `{"name":"general"}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}

	m, err := st.Get("channel", "chan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !m.Found || m.Status != "pending" || m.StoatID != "" || m.CanonicalState != `{"name":"general"}` {
		t.Fatalf("unexpected mapping after WritePending: %+v", m)
	}
}

func TestMappingStore_Confirm_SetsStoatIDAndActive(t *testing.T) {
	st := openTestDB(t)

	if err := st.WritePending("channel", "chan-1", `{"name":"general"}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := st.Confirm("channel", "chan-1", "stoat-chan-1"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	m, err := st.Get("channel", "chan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !m.Found || m.Status != "active" || m.StoatID != "stoat-chan-1" {
		t.Fatalf("unexpected mapping after Confirm: %+v", m)
	}
}

func TestMappingStore_WritePending_OnExistingActiveRow_PreservesStoatID(t *testing.T) {
	st := openTestDB(t)

	if err := st.WritePending("channel", "chan-1", `{"name":"old"}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := st.Confirm("channel", "chan-1", "stoat-chan-1"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Simulate an update: write-intent for the new desired state before the
	// remote call. A crash here must leave a recoverable row -- stoat_id is
	// not nulled out, only canonical_state/status change.
	if err := st.WritePending("channel", "chan-1", `{"name":"new"}`); err != nil {
		t.Fatalf("second WritePending: %v", err)
	}

	m, err := st.Get("channel", "chan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.Status != "pending" {
		t.Fatalf("expected status reset to pending during update write-intent, got %q", m.Status)
	}
	if m.StoatID != "stoat-chan-1" {
		t.Fatalf("expected existing stoat_id preserved across an update's pending window, got %q", m.StoatID)
	}
	if m.CanonicalState != `{"name":"new"}` {
		t.Fatalf("expected canonical_state updated to the new desired state, got %q", m.CanonicalState)
	}
}

func TestMappingStore_Remove_DeletesRow(t *testing.T) {
	st := openTestDB(t)

	if err := st.WritePending("channel", "chan-1", `{}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := st.Remove("channel", "chan-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	m, err := st.Get("channel", "chan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.Found {
		t.Fatalf("expected row gone after Remove, got %+v", m)
	}
}

func TestMappingStore_EachEntityTypeMapsToItsOwnTable(t *testing.T) {
	st := openTestDB(t)

	for _, entityType := range []string{"server", "category", "channel", "role", "emoji"} {
		if err := st.WritePending(entityType, "id-1", `{}`); err != nil {
			t.Fatalf("WritePending(%s): %v", entityType, err)
		}
	}

	for _, entityType := range []string{"server", "category", "channel", "role", "emoji"} {
		m, err := st.Get(entityType, "id-1")
		if err != nil {
			t.Fatalf("Get(%s): %v", entityType, err)
		}
		if !m.Found {
			t.Fatalf("expected row written to %s's own table, not found", entityType)
		}
	}
}

func TestMappingStore_UnknownEntityType_ReturnsError(t *testing.T) {
	st := openTestDB(t)

	if _, err := st.Get("bogus", "id-1"); err == nil {
		t.Fatal("expected an error for an entity type with no mapping table")
	}
}

func TestQueueStore_Enqueue_InsertsPendingRow(t *testing.T) {
	st := openTestDB(t)

	if err := st.Enqueue("channel.create", `{"discord_id":"chan-1"}`); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	var count int
	if err := st.DB.QueryRow(
		`SELECT COUNT(*) FROM op_queue WHERE op_type = ? AND status = 'pending'`, "channel.create",
	).Scan(&count); err != nil {
		t.Fatalf("query op_queue: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one pending op_queue row, got %d", count)
	}
}
