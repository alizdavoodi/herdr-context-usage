// Package sqlitestore is the one way this plugin opens an agent's SQLite store.
// It is shared by every store-backed provider because the read-only guarantee
// is a property of the open, not of any agent, and a second copy would be a
// second place for it to regress.
package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"

	_ "modernc.org/sqlite"
)

var (
	// ErrMissing is a store that has not been written yet: the agent has never
	// run, or has never run against this home. It is a lifecycle state, so
	// whatever is displayed for the pane is superseded.
	ErrMissing = errors.New("agent store has not been written yet")

	// ErrSchema is a file that exists but is not the store we were looking for,
	// which in practice means an initialised-but-empty database. Also a
	// lifecycle state rather than a read failure.
	ErrSchema = errors.New("agent store does not have the expected tables")
)

// OpenReadOnly opens an agent's store for reading and verifies it has the
// tables the caller needs.
//
// This is the whole store-open taxonomy for every caller: missing or
// uninitialised is semantic, and everything else — a permission error, a
// corrupt file, a directory where the database should be — is a transient
// failure that leaves the displayed value alone, because none of them is
// evidence about the agent session.
//
// mode=ro plus query_only(1) is what keeps this plugin from ever writing to an
// agent's database file, and both are verified to refuse DDL and DML under the
// pinned driver. It is not a guarantee that nothing beside the file changes:
// opening a WAL database whose -wal and -shm are absent, as after the agent's
// clean shutdown, creates both empty and leaves them after close, which SQLite
// needs in order to read the WAL at all. immutable=1 would avoid that, but it
// ignores the live WAL, so it would read a stale snapshot while the agent runs.
func OpenReadOnly(ctx context.Context, path string, tables ...string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrMissing
		}
		return nil, fmt.Errorf("stat agent store: %w", err)
	}

	dsn := &url.URL{Scheme: "file", Path: path}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	dsn.RawQuery = query.Encode()

	database, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open agent store: %w", err)
	}
	database.SetMaxOpenConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("open agent store read-only: %w", err)
	}
	if err := requireTables(ctx, database, tables); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func requireTables(ctx context.Context, database *sql.DB, tables []string) error {
	for _, table := range tables {
		var found int
		err := database.QueryRowContext(ctx,
			`SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSchema
		}
		if err != nil {
			return fmt.Errorf("inspect agent store schema: %w", err)
		}
	}
	return nil
}
