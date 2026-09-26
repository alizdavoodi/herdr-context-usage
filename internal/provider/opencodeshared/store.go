package opencodeshared

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"

	_ "modernc.org/sqlite"
)

var (
	// ErrMissing is a store that has not been written yet: the agent has never
	// run, or has never run against this data directory. It is a lifecycle
	// state, so whatever is displayed for the pane is superseded.
	ErrMissing = errors.New("opencode store has not been written yet")

	// ErrSchema is a file that exists but is not the store we were looking for,
	// which in practice means an initialised-but-empty database. Also a
	// lifecycle state rather than a read failure.
	ErrSchema = errors.New("opencode store does not have the expected tables")
)

// OpenReadOnly opens an opencode store for reading and verifies it has the
// tables the caller needs.
//
// This is the whole store-open taxonomy for both providers: missing or
// uninitialised is semantic, and everything else — a permission error, a
// corrupt file, a directory where the database should be — is a transient
// failure that leaves the displayed value alone, because none of them is
// evidence about the agent session.
//
// mode=ro plus query_only(1) is what keeps this plugin from ever writing to an
// agent's live database, and both are verified to refuse DDL and DML under the
// pinned driver.
func OpenReadOnly(ctx context.Context, path string, tables ...string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrMissing
		}
		return nil, fmt.Errorf("stat opencode store: %w", err)
	}

	dsn := &url.URL{Scheme: "file", Path: path}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	dsn.RawQuery = query.Encode()

	database, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open opencode store: %w", err)
	}
	database.SetMaxOpenConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("open opencode store read-only: %w", err)
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
			return fmt.Errorf("inspect opencode store schema: %w", err)
		}
	}
	return nil
}

// DataPath resolves one of opencode's store filenames. Both versions keep their
// databases side by side in the same directory and differ only in the name.
//
// dataHome is $XDG_DATA_HOME as main read it: opencode files its stores under
// that directory when it is set and under ~/.local/share otherwise, so it plays
// the part an agent's configured home plays for the other providers.
func DataPath(explicit, dataHome, filename string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	base, err := provider.Home(dataHome, filepath.Join(".local", "share"))
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "opencode", filename), nil
}
