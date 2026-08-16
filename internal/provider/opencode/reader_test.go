package opencode

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOccupancyReadsV1TablesFromReadOnlyDatabase(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_current')`)
		exec(t, database, `INSERT INTO session_v2 (id, tokens_input) VALUES ('ses_current', 999999)`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_old', 'ses_current', 1, '{"role":"assistant","time":{"completed":1},"tokens":{"input":3000,"cache":{"read":4000,"write":5000}}}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_user', 'ses_current', 2, '{"role":"user"}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_current', 'ses_current', 3, '{"role":"assistant","time":{"completed":3},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}')`)
	})
	legacyPath := filepath.Join(filepath.Dir(databasePath), "storage", "message", "stale.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"role":"assistant","tokens":{"input":999999,"cache":{"read":0,"write":0}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(databasePath, 0o444); err != nil {
		t.Fatal(err)
	}

	store := openStore(t, databasePath)
	defer store.Close()
	got, err := store.ReadOccupancy(context.Background(), "ses_current")
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got != 92_481 {
		t.Fatalf("ReadOccupancy() = %d, want 92481", got)
	}
}

func TestReadOccupancyTreatsZeroAsSemantic(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_zero')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_zero', 'ses_zero', 1, '{"role":"assistant","time":{"completed":1},"tokens":{"input":0,"cache":{"read":0,"write":0}}}')`)
	})

	store := openStore(t, databasePath)
	defer store.Close()
	if _, err := store.ReadOccupancy(context.Background(), "ses_zero"); !errors.Is(err, ErrNoOccupancy) {
		t.Fatalf("ReadOccupancy() error = %v, want ErrNoOccupancy", err)
	}
}

func TestReadOccupancyUsesPreviousCompletedReadingWhenLatestAssistantIsUnusable(t *testing.T) {
	t.Parallel()

	for _, latest := range []string{
		`{"role":"assistant","time":{"created":2}}`,
		`{"role":"assistant","time":{"completed":2}}`,
		`{"role":"assistant","time":{"completed":2},"tokens":{"input":-1,"cache":{"read":0,"write":0}}}`,
		`{"role":"assistant","time":{"completed":2},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`,
	} {
		databasePath := fixtureDatabase(t, func(database *sql.DB) {
			exec(t, database, `INSERT INTO session (id) VALUES ('ses_pending')`)
			exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_complete', 'ses_pending', 1, '{"role":"assistant","time":{"completed":1},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}')`)
			exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_pending', 'ses_pending', 2, ?)`, latest)
		})

		store := openStore(t, databasePath)
		got, err := store.ReadOccupancy(context.Background(), "ses_pending")
		store.Close()
		if err != nil || got != 92_481 {
			t.Fatalf("ReadOccupancy() = %d, %v; want prior completed occupancy", got, err)
		}
	}
}

func TestReadOccupancyDoesNotWalkBackAcrossLatestCompactionSummary(t *testing.T) {
	t.Parallel()

	for _, summary := range []string{`true`, `{}`} {
		databasePath := fixtureDatabase(t, func(database *sql.DB) {
			exec(t, database, `INSERT INTO session (id) VALUES ('ses_compacted')`)
			exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_before', 'ses_compacted', 1, '{"role":"assistant","time":{"completed":1},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}')`)
			exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_summary', 'ses_compacted', 2, ?)`, `{"role":"user","summary":`+summary+`}`)
		})
		store := openStore(t, databasePath)
		_, err := store.ReadOccupancy(context.Background(), "ses_compacted")
		store.Close()
		if !errors.Is(err, ErrJustCompacted) {
			t.Fatalf("ReadOccupancy() error = %v, want ErrJustCompacted", err)
		}
	}
}

func TestReadOccupancyRejectsMissingV1SchemaAndSession(t *testing.T) {
	t.Parallel()

	t.Run("v2 table alone is not v1", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "opencode.db")
		database, err := sql.Open("sqlite", databasePath)
		if err != nil {
			t.Fatal(err)
		}
		exec(t, database, `CREATE TABLE session_v2 (id TEXT PRIMARY KEY, tokens_input INTEGER)`)
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), databasePath); !errors.Is(err, ErrNoSchema) {
			t.Fatalf("Open() error = %v, want ErrNoSchema", err)
		}
	})

	t.Run("reported session no longer exists", func(t *testing.T) {
		databasePath := fixtureDatabase(t, func(database *sql.DB) {
			exec(t, database, `INSERT INTO session (id) VALUES ('ses_other')`)
		})
		store := openStore(t, databasePath)
		defer store.Close()
		if _, err := store.ReadOccupancy(context.Background(), "ses_current"); !errors.Is(err, ErrNoSession) {
			t.Fatalf("ReadOccupancy() error = %v, want ErrNoSession", err)
		}
	})
}

func TestResolveTerminalTitleMatchesDirectoryWithoutRecency(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id, directory, title) VALUES ('target', '/work/acme', 'Probe test response')`)
		exec(t, database, `INSERT INTO session (id, directory, title, parent_id) VALUES ('child', '/work/acme', 'Probe test response', 'target')`)
		exec(t, database, `INSERT INTO session (id, directory, title) VALUES ('wrong-title', '/work/acme', 'Newer unrelated session')`)
		exec(t, database, `INSERT INTO session (id, directory, title) VALUES ('wrong-directory', '/work/other', 'Probe test response')`)
	})
	store := openStore(t, databasePath)
	defer store.Close()

	resolved, err := store.ResolveTerminalTitle(context.Background(), "/work/acme", "OC | Probe test response")
	if err != nil {
		t.Fatalf("ResolveTerminalTitle() error = %v", err)
	}
	if resolved.ID != "target" || resolved.Mode != ResolutionTerminalTitle {
		t.Fatalf("ResolveTerminalTitle() = %#v", resolved)
	}
}

func TestResolveTerminalTitleToleratesTruncationAndRefusesGuesses(t *testing.T) {
	t.Parallel()

	prefix := strings.Repeat("long-title-", 4)[:37]
	tests := []struct {
		name          string
		directory     string
		terminalTitle string
		populate      func(*testing.T, *sql.DB)
		wantID        string
		wantErr       error
	}{
		{
			name:          "truncated title matches its unique prefix",
			directory:     "/work/acme",
			terminalTitle: "OC | " + prefix + "...",
			populate: func(t *testing.T, database *sql.DB) {
				exec(t, database, `INSERT INTO session (id, directory, title) VALUES ('target', '/work/acme', ?)`, prefix+"continuation")
			},
			wantID: "target",
		},
		{
			name:          "ambiguous truncated prefix stays blank",
			directory:     "/work/acme",
			terminalTitle: "OC | " + prefix + "...",
			populate: func(t *testing.T, database *sql.DB) {
				exec(t, database, `INSERT INTO session (id, directory, title) VALUES ('first', '/work/acme', ?)`, prefix+" first")
				exec(t, database, `INSERT INTO session (id, directory, title) VALUES ('second', '/work/acme', ?)`, prefix+" second")
			},
			wantErr: ErrNoSession,
		},
		{name: "bare title is untitled", directory: "/work/acme", terminalTitle: "OpenCode", wantErr: ErrUntitled},
		{name: "missing title prefix has no fallback", directory: "/work/acme", terminalTitle: "shell", wantErr: ErrNoSession},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fixtureDatabase(t, func(database *sql.DB) {
				if tt.populate != nil {
					tt.populate(t, database)
				}
			})
			store := openStore(t, path)
			defer store.Close()
			resolved, err := store.ResolveTerminalTitle(context.Background(), tt.directory, tt.terminalTitle)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveTerminalTitle() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantID != "" && resolved.ID != tt.wantID {
				t.Fatalf("ResolveTerminalTitle() = %#v, want %q", resolved, tt.wantID)
			}
		})
	}
}

func TestOpenMissingStoreIsSemantic(t *testing.T) {
	t.Parallel()

	if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "opencode.db")); !errors.Is(err, ErrStoreNotReady) {
		t.Fatalf("Open() error = %v, want ErrStoreNotReady", err)
	}
}

func TestOpenRejectsWrites(t *testing.T) {
	t.Parallel()

	store := openStore(t, fixtureDatabase(t, func(*sql.DB) {}))
	defer store.Close()
	if _, err := store.database.ExecContext(context.Background(), `INSERT INTO session (id) VALUES ('write')`); err == nil {
		t.Fatal("read-only opencode store accepted a write")
	}
}

func TestDatabasePathUsesExplicitPath(t *testing.T) {
	t.Parallel()

	want := filepath.Join(t.TempDir(), "opencode.db")
	got, err := databasePath(want)
	if err != nil {
		t.Fatalf("databasePath() error = %v", err)
	}
	if got != want {
		t.Fatalf("databasePath() = %q, want %q", got, want)
	}
}

func TestDatabasePathUsesXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg-data")

	got, err := databasePath("")
	if err != nil {
		t.Fatalf("databasePath() error = %v", err)
	}
	if want := filepath.Join("/xdg-data", "opencode", "opencode.db"); got != want {
		t.Fatalf("databasePath() = %q, want %q", got, want)
	}
}

func fixtureDatabase(t *testing.T, populate func(*sql.DB)) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "opencode.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, database, `CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, title TEXT, parent_id TEXT)`)
	exec(t, database, `CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`)
	exec(t, database, `CREATE TABLE session_v2 (id TEXT PRIMARY KEY, tokens_input INTEGER)`)
	populate(database)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func exec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
