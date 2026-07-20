package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// busyTimeoutMS bounds how long a connection waits on SQLITE_BUSY (another
// connection holding the write lock) before returning the error. SQLite
// allows exactly one writer at a time regardless of this setting; capping
// the pool to one connection (below) means only concurrent readers would
// ever need this, but it stays as a backstop.
const busyTimeoutMS = 5000

// Store wraps the SQLite connection used by every mapping table.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies
// any migrations not yet recorded in schema_migrations. This runs on every
// startup, so a deploy that ships new migration files is picked up
// automatically on next boot without a separate upgrade step.
func Open(path string) (*Store, error) {
	return openWithMigrations(path, migrationsFS, migrationsDir)
}

func openWithMigrations(path string, fsys fs.FS, dir string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	// SQLite allows exactly one writer at a time. database/sql's default
	// pool opens multiple connections and lets them race for that lock,
	// so concurrent writers (e.g. the engine's per-entity workers firing
	// on a burst of role events) see SQLITE_BUSY immediately instead of
	// queuing. Capping the pool to one connection serializes access at
	// the pool level; the busy_timeout pragma is the backstop for any
	// contention that still occurs (e.g. a reader mid-transaction).
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMS)); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: set busy_timeout %s: %w", path, err)
	}

	if err := applyMigrations(db, fsys, dir); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate %s: %w", path, err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func applyMigrations(db *sql.DB, fsys fs.FS, dir string) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL DEFAULT (unixepoch())
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE filename = ?`, name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}

		sqlBytes, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := db.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := db.Exec(
			`INSERT INTO schema_migrations (filename) VALUES (?)`, name,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}

	return nil
}
