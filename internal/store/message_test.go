package store

import "testing"

// seedChannelForMessage writes an active channel_map row so message_map's
// FK constraint on channel_id is satisfied.
func seedChannelForMessage(t *testing.T, st *Store, channelID string) {
	t.Helper()
	if err := st.WritePending("channel", channelID, `{}`); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

func TestMappingStore_Message_Get_NotFoundReturnsZeroValue(t *testing.T) {
	st := openTestDB(t)

	m, err := st.Get("message", "msg-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.Found {
		t.Fatalf("expected Found=false for a row that was never written, got %+v", m)
	}
}

func TestMappingStore_Message_WritePending_ThenGet_ReturnsPendingRowWithNoStoatID(t *testing.T) {
	st := openTestDB(t)
	seedChannelForMessage(t, st, "chan-1")

	if err := st.WritePending("message", "msg-1", `{"channel_id":"chan-1","content":"hi"}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}

	m, err := st.Get("message", "msg-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !m.Found || m.Status != "pending" || m.StoatID != "" || m.CanonicalState != `{"channel_id":"chan-1","content":"hi"}` {
		t.Fatalf("unexpected mapping after WritePending: %+v", m)
	}
}

func TestMappingStore_Message_Confirm_SetsStoatIDAndActive(t *testing.T) {
	st := openTestDB(t)
	seedChannelForMessage(t, st, "chan-1")

	if err := st.WritePending("message", "msg-1", `{"channel_id":"chan-1"}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := st.Confirm("message", "msg-1", "stoat-msg-1"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	m, err := st.Get("message", "msg-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !m.Found || m.Status != "active" || m.StoatID != "stoat-msg-1" {
		t.Fatalf("unexpected mapping after Confirm: %+v", m)
	}
}

// An edit's write-pending must not touch channel_id or stoat_id -- only
// canonical_state changes, mirroring the generic WritePending's
// stoat_id-preservation behavior.
func TestMappingStore_Message_WritePending_OnExistingRow_LeavesChannelIDAndStoatIDUntouched(t *testing.T) {
	st := openTestDB(t)
	seedChannelForMessage(t, st, "chan-1")

	if err := st.WritePending("message", "msg-1", `{"channel_id":"chan-1","content":"old"}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := st.Confirm("message", "msg-1", "stoat-msg-1"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Edit: a second WritePending with a different (even bogus) channel_id
	// in the payload must not move the row to a different channel.
	if err := st.WritePending("message", "msg-1", `{"channel_id":"chan-does-not-exist","content":"new"}`); err != nil {
		t.Fatalf("second WritePending: %v", err)
	}

	m, err := st.Get("message", "msg-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.StoatID != "stoat-msg-1" {
		t.Fatalf("expected stoat_id preserved across edit's pending window, got %q", m.StoatID)
	}
	if m.CanonicalState != `{"channel_id":"chan-does-not-exist","content":"new"}` {
		t.Fatalf("expected canonical_state updated to new desired state, got %q", m.CanonicalState)
	}

	var channelID string
	if err := st.db.QueryRow(`SELECT channel_id FROM message_map WHERE discord_msg_id = ?`, "msg-1").Scan(&channelID); err != nil {
		t.Fatalf("query channel_id: %v", err)
	}
	if channelID != "chan-1" {
		t.Fatalf("channel_id changed on edit, got %q, want chan-1 (FK set only on first insert)", channelID)
	}
}

func TestMappingStore_Message_Remove_DeletesRow(t *testing.T) {
	st := openTestDB(t)
	seedChannelForMessage(t, st, "chan-1")

	if err := st.WritePending("message", "msg-1", `{"channel_id":"chan-1"}`); err != nil {
		t.Fatalf("WritePending: %v", err)
	}
	if err := st.Remove("message", "msg-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	m, err := st.Get("message", "msg-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.Found {
		t.Fatalf("expected row gone after Remove, got %+v", m)
	}
}

// Documents the NOT NULL FK constraint the design relies on: a message
// referencing a channel with no channel_map row must fail loudly, not
// silently insert an orphan.
func TestMappingStore_Message_WritePending_UnknownChannelID_ReturnsError(t *testing.T) {
	st := openTestDB(t)

	err := st.WritePending("message", "msg-1", `{"channel_id":"chan-does-not-exist"}`)
	if err == nil {
		t.Fatal("expected an FK-violation error for a channel_id with no channel_map row")
	}
}

// Regression: the generic five-table dispatch must still work unaffected by
// message's own special-cased branch in Get/WritePending/Confirm/Remove.
func TestMappingStore_GenericEntityTypes_StillDispatchCorrectly(t *testing.T) {
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
		t.Fatalf("generic dispatch broken: %+v", m)
	}

	if err := st.Remove("channel", "chan-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	m, err = st.Get("channel", "chan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.Found {
		t.Fatalf("expected channel row gone after Remove, got %+v", m)
	}
}
