package store

import (
	"database/sql"
	"fmt"
)

// Mapping row status values. Mirrors internal/engine's status vocabulary
// without importing it, so the dependency stays one-directional (engine
// depends on Store, not the reverse).
const (
	statusPending = "pending"
	statusActive  = "active"
)

// mappingTables maps each canonical entity type (spec 3) to its table name.
// All five share the same shape (discord_id, stoat_id, status,
// canonical_state, timestamps); message_map has its own shape and is out of
// scope for this generic interface.
var mappingTables = map[string]string{
	"server":   "server_map",
	"category": "category_map",
	"channel":  "channel_map",
	"role":     "role_map",
	"emoji":    "emoji_map",
}

// Mapping is a read of one entity's row in its mapping table. Mirrors
// internal/engine.Mapping's shape without importing internal/engine, so the
// dependency stays one-directional (engine depends on Store, not the
// reverse).
type Mapping struct {
	Found          bool
	StoatID        string
	Status         string
	CanonicalState string
}

func tableFor(entityType string) (string, error) {
	table, ok := mappingTables[entityType]
	if !ok {
		return "", fmt.Errorf("store: unknown entity type %q", entityType)
	}
	return table, nil
}

// Get reads one entity's mapping row, or a zero Mapping{Found: false} if it
// doesn't exist yet.
func (s *Store) Get(entityType, discordID string) (Mapping, error) {
	table, err := tableFor(entityType)
	if err != nil {
		return Mapping{}, err
	}

	var stoatID sql.NullString
	var status, canonicalState string
	err = s.db.QueryRow(
		`SELECT stoat_id, status, canonical_state FROM `+table+` WHERE discord_id = ?`, discordID,
	).Scan(&stoatID, &status, &canonicalState)
	if err == sql.ErrNoRows {
		return Mapping{}, nil
	}
	if err != nil {
		return Mapping{}, fmt.Errorf("store: get %s %s: %w", entityType, discordID, err)
	}

	return Mapping{
		Found:          true,
		StoatID:        stoatID.String,
		Status:         status,
		CanonicalState: canonicalState,
	}, nil
}

// WritePending is the write-intent half of the pending-row pattern (spec 8):
// insert a new row (stoat_id NULL, status pending) for a first-time create,
// or mark an existing row pending with the new desired canonical_state for
// an update -- preserving its existing stoat_id, so an update's pending
// window stays recoverable without losing the entity's identity on Stoat.
func (s *Store) WritePending(entityType, discordID, canonicalState string) error {
	table, err := tableFor(entityType)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`INSERT INTO `+table+` (discord_id, stoat_id, status, canonical_state)
		 VALUES (?, NULL, ?, ?)
		 ON CONFLICT (discord_id) DO UPDATE SET
		   status = ?,
		   canonical_state = excluded.canonical_state,
		   updated_at = unixepoch()`,
		discordID, statusPending, canonicalState, statusPending,
	)
	if err != nil {
		return fmt.Errorf("store: write pending %s %s: %w", entityType, discordID, err)
	}
	return nil
}

// Confirm is the confirm half of the pending-row pattern: the remote call
// succeeded, so record the returned stoat_id and mark the row active.
func (s *Store) Confirm(entityType, discordID, stoatID string) error {
	table, err := tableFor(entityType)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`UPDATE `+table+` SET stoat_id = ?, status = ?, updated_at = unixepoch() WHERE discord_id = ?`,
		stoatID, statusActive, discordID,
	)
	if err != nil {
		return fmt.Errorf("store: confirm %s %s: %w", entityType, discordID, err)
	}
	return nil
}

// Remove deletes an entity's mapping row entirely, for a confirmed delete.
func (s *Store) Remove(entityType, discordID string) error {
	table, err := tableFor(entityType)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`DELETE FROM `+table+` WHERE discord_id = ?`, discordID)
	if err != nil {
		return fmt.Errorf("store: remove %s %s: %w", entityType, discordID, err)
	}
	return nil
}

// Enqueue persists an op that couldn't be applied while degraded, for later
// durable-queue drain (Phase 7).
func (s *Store) Enqueue(opType, payload string) error {
	_, err := s.db.Exec(
		`INSERT INTO op_queue (op_type, payload, status) VALUES (?, ?, ?)`,
		opType, payload, statusPending,
	)
	if err != nil {
		return fmt.Errorf("store: enqueue %s: %w", opType, err)
	}
	return nil
}
