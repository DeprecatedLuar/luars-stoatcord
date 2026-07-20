package store

import (
	"path/filepath"
	"testing"
	"testing/fstest"
)

func openTestDB(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpen_FreshDB_CreatesAllTables(t *testing.T) {
	st := openTestDB(t)

	want := []string{
		"server_map", "category_map", "channel_map", "role_map",
		"emoji_map", "message_map", "channel_cursor", "op_queue",
	}
	for _, table := range want {
		var name string
		err := st.DB.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s: not found: %v", table, err)
		}
	}
}

func TestSchema_RoundTripsSampleRowPerMappingTable(t *testing.T) {
	st := openTestDB(t)

	for _, table := range []string{
		"server_map", "category_map", "channel_map", "role_map", "emoji_map",
	} {
		_, err := st.DB.Exec(
			`INSERT INTO `+table+` (discord_id, stoat_id, status, canonical_state) VALUES (?, ?, ?, ?)`,
			"discord-1", nil, "pending", `{"name":"sample"}`,
		)
		if err != nil {
			t.Fatalf("insert into %s: %v", table, err)
		}

		var discordID, status string
		err = st.DB.QueryRow(
			`SELECT discord_id, status FROM `+table+` WHERE discord_id = ?`, "discord-1",
		).Scan(&discordID, &status)
		if err != nil {
			t.Fatalf("select from %s: %v", table, err)
		}
		if discordID != "discord-1" || status != "pending" {
			t.Errorf("%s round-trip mismatch: got (%s, %s)", table, discordID, status)
		}
	}
}

func TestSchema_RoundTripsMessageMapAndChannelCursor(t *testing.T) {
	st := openTestDB(t)

	if _, err := st.DB.Exec(
		`INSERT INTO channel_map (discord_id, status, canonical_state) VALUES (?, ?, ?)`,
		"channel-1", "active", "{}",
	); err != nil {
		t.Fatalf("insert channel_map: %v", err)
	}

	if _, err := st.DB.Exec(
		`INSERT INTO message_map (discord_msg_id, stoat_msg_id, channel_id) VALUES (?, ?, ?)`,
		"msg-1", "stoat-msg-1", "channel-1",
	); err != nil {
		t.Fatalf("insert message_map: %v", err)
	}

	var stoatMsgID string
	if err := st.DB.QueryRow(
		`SELECT stoat_msg_id FROM message_map WHERE discord_msg_id = ?`, "msg-1",
	).Scan(&stoatMsgID); err != nil {
		t.Fatalf("select message_map: %v", err)
	}
	if stoatMsgID != "stoat-msg-1" {
		t.Errorf("message_map round-trip mismatch: got %s", stoatMsgID)
	}

	if _, err := st.DB.Exec(
		`INSERT INTO channel_cursor (channel_id, last_synced_discord_msg_id) VALUES (?, ?)`,
		"channel-1", "msg-1",
	); err != nil {
		t.Fatalf("insert channel_cursor: %v", err)
	}

	var cursor string
	if err := st.DB.QueryRow(
		`SELECT last_synced_discord_msg_id FROM channel_cursor WHERE channel_id = ?`, "channel-1",
	).Scan(&cursor); err != nil {
		t.Fatalf("select channel_cursor: %v", err)
	}
	if cursor != "msg-1" {
		t.Errorf("channel_cursor round-trip mismatch: got %s", cursor)
	}
}

func TestSchema_RoundTripsOpQueue(t *testing.T) {
	st := openTestDB(t)

	res, err := st.DB.Exec(
		`INSERT INTO op_queue (op_type, payload, status) VALUES (?, ?, ?)`,
		"channel.create", `{"discord_id":"channel-1"}`, "pending",
	)
	if err != nil {
		t.Fatalf("insert op_queue: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}

	var opType, status string
	if err := st.DB.QueryRow(
		`SELECT op_type, status FROM op_queue WHERE id = ?`, id,
	).Scan(&opType, &status); err != nil {
		t.Fatalf("select op_queue: %v", err)
	}
	if opType != "channel.create" || status != "pending" {
		t.Errorf("op_queue round-trip mismatch: got (%s, %s)", opType, status)
	}
}

// TestDeployedMigration_AddsColumnWithoutLosingExistingData proves the
// pre-run migration hook (applyMigrations, run on every store.Open) is safe
// to ship a schema update through: a later deploy that adds a new migration
// file picks it up automatically on next boot, and existing rows survive.
func TestDeployedMigration_AddsColumnWithoutLosingExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	v1 := fstest.MapFS{
		"migrations/0001_widget.sql": &fstest.MapFile{
			Data: []byte(`CREATE TABLE widget (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`),
		},
	}

	db1, err := openWithMigrations(path, v1, "migrations")
	if err != nil {
		t.Fatalf("open v1: %v", err)
	}
	if _, err := db1.DB.Exec(`INSERT INTO widget (id, name) VALUES (1, 'pre-existing')`); err != nil {
		t.Fatalf("insert pre-existing row: %v", err)
	}
	db1.Close()

	// Simulate a deploy that ships an additional migration file.
	v2 := fstest.MapFS{
		"migrations/0001_widget.sql": v1["migrations/0001_widget.sql"],
		"migrations/0002_widget_description.sql": &fstest.MapFile{
			Data: []byte(`ALTER TABLE widget ADD COLUMN description TEXT;`),
		},
	}

	db2, err := openWithMigrations(path, v2, "migrations")
	if err != nil {
		t.Fatalf("open v2: %v", err)
	}
	defer db2.Close()

	var name string
	if err := db2.DB.QueryRow(`SELECT name FROM widget WHERE id = 1`).Scan(&name); err != nil {
		t.Fatalf("pre-existing row lost after migration: %v", err)
	}
	if name != "pre-existing" {
		t.Errorf("pre-existing row corrupted: got name %q", name)
	}

	if _, err := db2.DB.Exec(
		`INSERT INTO widget (id, name, description) VALUES (2, 'new', 'has description column')`,
	); err != nil {
		t.Errorf("new column from deployed migration not usable: %v", err)
	}

	var count int
	if err := db2.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE filename = ?`, "0001_widget.sql").Scan(&count); err != nil {
		t.Fatalf("check schema_migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("0001_widget.sql should be recorded exactly once, got count %d (re-applied?)", count)
	}
}

func TestOpen_Idempotent_ReapplyingMigrationsDoesNotFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	st1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	st1.Close()

	st2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer st2.Close()
}
