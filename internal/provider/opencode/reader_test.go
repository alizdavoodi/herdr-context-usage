package opencode

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOccupancyReadsV1TablesFromReadOnlyDatabase(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_current')`)
		exec(t, database, `INSERT INTO session_v2 (id, tokens_input) VALUES ('ses_current', 999999)`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_old', 'ses_current', 1, '{"role":"assistant","tokens":{"input":3000,"cache":{"read":4000,"write":5000}}}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_user', 'ses_current', 2, '{"role":"user"}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_current', 'ses_current', 3, '{"role":"assistant","tokens":{"input":840,"cache":{"read":90000,"write":1641}}}')`)
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

	got, err := ReadOccupancy(databasePath, "ses_current")
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got != 92_481 {
		t.Fatalf("ReadOccupancy() = %d, want 92481", got)
	}
}

func TestReadOccupancyKeepsZeroAsARealReading(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_zero')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_zero', 'ses_zero', 1, '{"role":"assistant","tokens":{"input":0,"cache":{"read":0,"write":0}}}')`)
	})

	got, err := ReadOccupancy(databasePath, "ses_zero")
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got != 0 {
		t.Fatalf("ReadOccupancy() = %d, want 0", got)
	}
}

func TestReadOccupancyTreatsMissingLatestAssistantTokensAsSemantic(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_pending')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_complete', 'ses_pending', 1, '{"role":"assistant","tokens":{"input":840,"cache":{"read":90000,"write":1641}}}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_pending', 'ses_pending', 2, '{"role":"assistant"}')`)
	})

	if _, err := ReadOccupancy(databasePath, "ses_pending"); !errors.Is(err, ErrNoOccupancy) {
		t.Fatalf("ReadOccupancy() error = %v, want ErrNoOccupancy", err)
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
		if _, err := ReadOccupancy(databasePath, "ses_current"); !errors.Is(err, ErrNoSchema) {
			t.Fatalf("ReadOccupancy() error = %v, want ErrNoSchema", err)
		}
	})

	t.Run("reported session no longer exists", func(t *testing.T) {
		databasePath := fixtureDatabase(t, func(database *sql.DB) {
			exec(t, database, `INSERT INTO session (id) VALUES ('ses_other')`)
		})
		if _, err := ReadOccupancy(databasePath, "ses_current"); !errors.Is(err, ErrNoSession) {
			t.Fatalf("ReadOccupancy() error = %v, want ErrNoSession", err)
		}
	})
}

func TestDatabasePathUsesConfiguredDataDirectory(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	got, err := DatabasePath(dataDir)
	if err != nil {
		t.Fatalf("DatabasePath() error = %v", err)
	}
	if want := filepath.Join(dataDir, "opencode.db"); got != want {
		t.Fatalf("DatabasePath() = %q, want %q", got, want)
	}
}

func fixtureDatabase(t *testing.T, populate func(*sql.DB)) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "opencode.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, database, `CREATE TABLE session (id TEXT PRIMARY KEY)`)
	exec(t, database, `CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`)
	exec(t, database, `CREATE TABLE session_v2 (id TEXT PRIMARY KEY, tokens_input INTEGER)`)
	populate(database)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func exec(t *testing.T, database *sql.DB, query string) {
	t.Helper()
	if _, err := database.Exec(query); err != nil {
		t.Fatal(err)
	}
}
