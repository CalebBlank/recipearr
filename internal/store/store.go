// Package store is the SQLite persistence layer (pure-Go modernc driver, no CGO).
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sources (
	id                    INTEGER PRIMARY KEY AUTOINCREMENT,
	name                  TEXT NOT NULL,
	feed_url              TEXT NOT NULL,
	site_url              TEXT NOT NULL DEFAULT '',
	enabled               INTEGER NOT NULL DEFAULT 1,
	poll_interval_minutes INTEGER NOT NULL DEFAULT 60,
	backlog_on_add        INTEGER NOT NULL DEFAULT 1,
	backlog_limit         INTEGER NOT NULL DEFAULT 25,
	seeded                INTEGER NOT NULL DEFAULT 0,
	default_keywords      TEXT NOT NULL DEFAULT '',
	created_at            TEXT NOT NULL,
	last_checked_at       TEXT,
	last_status           TEXT NOT NULL DEFAULT '',
	last_error            TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS filter_rules (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	source_id  INTEGER REFERENCES sources(id) ON DELETE CASCADE,
	mode       TEXT NOT NULL,
	field      TEXT NOT NULL DEFAULT 'any',
	keyword    TEXT NOT NULL,
	match_type TEXT NOT NULL DEFAULT 'contains',
	enabled    INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS items (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	source_id         INTEGER REFERENCES sources(id) ON DELETE SET NULL,
	guid              TEXT NOT NULL DEFAULT '',
	url               TEXT NOT NULL,
	title             TEXT NOT NULL DEFAULT '',
	status            TEXT NOT NULL,
	filter_reason     TEXT NOT NULL DEFAULT '',
	tandoor_recipe_id INTEGER,
	raw_tags          TEXT NOT NULL DEFAULT '',
	error             TEXT NOT NULL DEFAULT '',
	discovered_at     TEXT NOT NULL,
	processed_at      TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_items_source_guid ON items(source_id, guid) WHERE guid != '';
CREATE INDEX IF NOT EXISTS idx_items_url ON items(url);
CREATE INDEX IF NOT EXISTS idx_items_status ON items(status);
`

// Open opens (creating if needed) the SQLite database at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Pure-Go driver: one writer at a time. Serialize and wait on locks.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate brings an existing database up to date. CREATE TABLE IF NOT EXISTS won't add
// columns to a table that already exists, so newly-introduced columns are added here.
// Idempotent: each column is only added when missing.
func migrate(db *sql.DB) error {
	type col struct{ table, name, ddl string }
	added := []col{
		{"sources", "seeded", "seeded INTEGER NOT NULL DEFAULT 0"},
	}
	for _, c := range added {
		if err := ensureColumn(db, c.table, c.name, c.ddl); err != nil {
			return err
		}
	}
	return nil
}

func ensureColumn(db *sql.DB, table, column, ddl string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + ddl)
	return err
}

// ---- settings (key/value) ----

// GetSetting returns the value for key, or def if the key is unset.
func (s *Store) GetSetting(key, def string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	return v, nil
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}
