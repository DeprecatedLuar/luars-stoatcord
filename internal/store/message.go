package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// entityTypeMessage is message_map's dispatch key -- kept out of
// mappingTables since message_map does not share the five common-shape
// tables' columns (see mappingTables' doc comment).
const entityTypeMessage = "message"

// messageCanonicalFields decodes just the FK value out of a message's
// canonical_state JSON, needed for the table's own channel_id column on
// first insert.
type messageCanonicalFields struct {
	ChannelID string `json:"channel_id"`
}

// getMessage reads one message's mapping row. Status is derived from
// stoat_msg_id nullability -- message_map has no status column of its own.
func (s *Store) getMessage(discordID string) (Mapping, error) {
	var stoatID sql.NullString
	var canonicalState string
	err := s.db.QueryRow(
		`SELECT stoat_msg_id, canonical_state FROM message_map WHERE discord_msg_id = ?`, discordID,
	).Scan(&stoatID, &canonicalState)
	if err == sql.ErrNoRows {
		return Mapping{}, nil
	}
	if err != nil {
		return Mapping{}, fmt.Errorf("store: get message %s: %w", discordID, err)
	}

	status := statusPending
	if stoatID.Valid && stoatID.String != "" {
		status = statusActive
	}
	return Mapping{
		Found:          true,
		StoatID:        stoatID.String,
		Status:         status,
		CanonicalState: canonicalState,
	}, nil
}

// writePendingMessage is the write-intent half of the pending-row pattern
// for messages: first-time create inserts channel_id (from the canonical
// state payload) and leaves stoat_msg_id NULL; an edit only updates
// canonical_state, leaving channel_id and stoat_msg_id untouched.
func (s *Store) writePendingMessage(discordID, canonicalState string) error {
	var fields messageCanonicalFields
	if err := json.Unmarshal([]byte(canonicalState), &fields); err != nil {
		return fmt.Errorf("store: write pending message %s: decode channel_id: %w", discordID, err)
	}

	_, err := s.db.Exec(
		`INSERT INTO message_map (discord_msg_id, stoat_msg_id, channel_id, canonical_state)
		 VALUES (?, NULL, ?, ?)
		 ON CONFLICT (discord_msg_id) DO UPDATE SET
		   canonical_state = excluded.canonical_state`,
		discordID, fields.ChannelID, canonicalState,
	)
	if err != nil {
		return fmt.Errorf("store: write pending message %s: %w", discordID, err)
	}
	return nil
}

// confirmMessage records the returned stoat_msg_id once the remote send/edit
// succeeded.
func (s *Store) confirmMessage(discordID, stoatID string) error {
	_, err := s.db.Exec(`UPDATE message_map SET stoat_msg_id = ? WHERE discord_msg_id = ?`, stoatID, discordID)
	if err != nil {
		return fmt.Errorf("store: confirm message %s: %w", discordID, err)
	}
	return nil
}

// removeMessage deletes a message's mapping row entirely, for a confirmed
// delete.
func (s *Store) removeMessage(discordID string) error {
	_, err := s.db.Exec(`DELETE FROM message_map WHERE discord_msg_id = ?`, discordID)
	if err != nil {
		return fmt.Errorf("store: remove message %s: %w", discordID, err)
	}
	return nil
}
