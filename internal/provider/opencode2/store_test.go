package opencode2

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestResolveMatchesDirectoryAndTerminalTitle(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{
		ID: "target", Directory: "/work/acme", Title: "Probe test response", Updated: 100,
	})
	insertSession(t, db, testSession{
		ID: "newest-wrong-title", Directory: "/work/acme", Title: "Unrelated newer session", Updated: 300,
	})
	insertSession(t, db, testSession{
		ID: "same-title-wrong-directory", Directory: "/work/other", Title: "Probe test response", Updated: 400,
	})
	insertSession(t, db, testSession{
		ID: "child", Directory: "/work/acme", Title: "Probe test response", ParentID: "target", Updated: 500,
	})
	db.Close()

	store := openStore(t, path)
	defer store.Close()

	got, err := store.Resolve(context.Background(), "/work/acme", "OC | Probe test response")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.ID != "target" || got.Mode != ResolutionTerminalTitle {
		t.Fatalf("Resolve() = %#v, want target resolved by terminal_title", got)
	}
}

func TestResolveToleratesTruncatedTitles(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	prefix := strings.Repeat("long-title-", 4)[:37]
	insertSession(t, db, testSession{ID: "long", Directory: "/work/acme", Title: prefix + "continuation", Updated: 100})
	db.Close()

	store := openStore(t, path)
	defer store.Close()

	got, err := store.Resolve(context.Background(), "/work/acme", "OC | "+prefix+"...")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.ID != "long" || got.Mode != ResolutionTerminalTitle {
		t.Fatalf("Resolve() = %#v", got)
	}
}

func TestResolveRefusesGuesses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		directory     string
		terminalTitle string
		prepare       func(*testing.T, *sql.DB)
		wantErr       error
	}{
		{
			name:          "bare OpenCode title is deliberately untitled",
			directory:     "/work/acme",
			terminalTitle: "OpenCode",
			wantErr:       ErrUntitled,
		},
		{
			name:          "missing title prefix does not fall back to recency",
			directory:     "/work/acme",
			terminalTitle: "shell",
			prepare: func(t *testing.T, db *sql.DB) {
				insertSession(t, db, testSession{ID: "newest", Directory: "/work/acme", Title: "Newest", Updated: 999})
			},
			wantErr: ErrNoSession,
		},
		{
			name:          "unmatched title does not fall back to recency",
			directory:     "/work/acme",
			terminalTitle: "OC | Not present",
			prepare: func(t *testing.T, db *sql.DB) {
				insertSession(t, db, testSession{ID: "newest", Directory: "/work/acme", Title: "Newest", Updated: 999})
			},
			wantErr: ErrNoSession,
		},
		{
			name:          "ambiguous truncated prefix stays blank",
			directory:     "/work/acme",
			terminalTitle: "OC | " + strings.Repeat("x", 37) + "...",
			prepare: func(t *testing.T, db *sql.DB) {
				insertSession(t, db, testSession{ID: "older", Directory: "/work/acme", Title: strings.Repeat("x", 37) + " old", Updated: 100})
				insertSession(t, db, testSession{ID: "newer", Directory: "/work/acme", Title: strings.Repeat("x", 37) + " new", Updated: 200})
			},
			wantErr: ErrNoSession,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := newTestDatabase(t)
			db := openFixtureDatabase(t, path)
			if tt.prepare != nil {
				tt.prepare(t, db)
			}
			db.Close()

			store := openStore(t, path)
			defer store.Close()
			if _, err := store.Resolve(context.Background(), tt.directory, tt.terminalTitle); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Resolve() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestReadOccupancyUsesLatestAssistantPayload(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{
		ID: "session", Directory: "/work/acme", Title: "Measured",
		TokensInput: 900_000, TokensCacheRead: 800_000, TokensCacheWrite: 700_000,
	})
	insertMessage(t, db, "old-assistant", "session", "assistant", 1,
		`{"time":{"completed":1},"model":{"id":"old-model"},"tokens":{"input":100,"cache":{"read":200,"write":300}}}`)
	insertMessage(t, db, "newer-user", "session", "user", 3,
		`{"role":"assistant","model":{"id":"wrong-role"},"tokens":{"input":999999,"cache":{"read":999999,"write":999999}}}`)
	insertMessage(t, db, "current-assistant", "session", "assistant", 2,
		`{"time":{"completed":2},"model":{"id":"deepseek-v4-pro"},"tokens":{"input":14829,"output":100,"cache":{"read":16896,"write":7}}}`)
	db.Close()

	store := openStore(t, path)
	defer store.Close()
	got, err := store.ReadOccupancy(context.Background(), "session")
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got.Occupancy != 31_732 {
		t.Fatalf("occupancy = %d, want input + cache read + cache write = 31732", got.Occupancy)
	}
	if got.ModelID != "deepseek-v4-pro" {
		t.Fatalf("model id = %q, want value from data.model.id", got.ModelID)
	}
}

func TestReadOccupancyWalksBackAcrossUnusableAssistantsWithoutCompaction(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{ID: "session", Directory: "/work", Title: "No occupancy yet"})
	insertMessage(t, db, "old", "session", "assistant", 1,
		`{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":129000,"cache":{"read":0,"write":0}}}`)
	insertMessage(t, db, "in-flight", "session", "assistant", 2,
		`{"time":{"created":2},"model":{"id":"model"},"content":[]}`)
	insertMessage(t, db, "failed", "session", "assistant", 3,
		`{"time":{"completed":3},"model":{"id":"model"},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`)
	insertMessage(t, db, "malformed", "session", "assistant", 4, `{`)
	db.Close()

	store := openStore(t, path)
	defer store.Close()
	got, err := store.ReadOccupancy(context.Background(), "session")
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got.Occupancy != 129_000 {
		t.Fatalf("occupancy = %d, want previous completed lower bound", got.Occupancy)
	}
}

func TestReadOccupancyStopsAtBuriedCompaction(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		status  string
		wantErr error
	}{
		{status: "running", wantErr: ErrNoOccupancy},
		{status: "completed", wantErr: ErrJustCompacted},
	} {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			path := newTestDatabase(t)
			db := openFixtureDatabase(t, path)
			insertSession(t, db, testSession{ID: "session", Directory: "/work", Title: "Compacted"})
			insertMessage(t, db, "pre-compaction", "session", "assistant", 1,
				`{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":225000,"cache":{"read":9000,"write":0}}}`)
			insertMessage(t, db, "compaction", "session", "compaction", 2,
				fmt.Sprintf(`{"status":%q,"reason":"auto","summary":"summary","recent":"recent"}`, tt.status))
			insertMessage(t, db, "malformed", "session", "assistant", 3, `{`)
			insertMessage(t, db, "failed", "session", "assistant", 4,
				`{"time":{"completed":4},"model":{"id":"model"},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`)
			insertMessage(t, db, "in-flight", "session", "assistant", 5,
				`{"time":{"created":5},"model":{"id":"model"},"content":[]}`)
			db.Close()

			store := openStore(t, path)
			defer store.Close()
			if _, err := store.ReadOccupancy(context.Background(), "session"); !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReadOccupancy() error = %v, want %v instead of pre-compaction occupancy", err, tt.wantErr)
			}
		})
	}
}

func TestReadOccupancyWalksBackAcrossFailedCompaction(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{ID: "session", Directory: "/work", Title: "Failed compaction"})
	insertMessage(t, db, "current", "session", "assistant", 1,
		`{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":120000,"cache":{"read":9000,"write":0}}}`)
	insertMessage(t, db, "failed-compaction", "session", "compaction", 2,
		`{"status":"failed","reason":"auto","error":{"message":"context deadline exceeded"}}`)
	insertMessage(t, db, "in-flight", "session", "assistant", 3,
		`{"time":{"created":3},"model":{"id":"model"},"content":[]}`)
	db.Close()

	store := openStore(t, path)
	defer store.Close()
	got, err := store.ReadOccupancy(context.Background(), "session")
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got.Occupancy != 129_000 {
		t.Fatalf("occupancy = %d, want unchanged pre-attempt occupancy", got.Occupancy)
	}
}

func TestReadOccupancyBoundsWalkBack(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{ID: "session", Directory: "/work", Title: "Bounded"})
	insertMessage(t, db, "too-old", "session", "assistant", 1,
		`{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":129000,"cache":{"read":0,"write":0}}}`)
	for seq := int64(2); seq <= 257; seq++ {
		insertMessage(t, db, fmt.Sprintf("in-flight-%d", seq), "session", "assistant", seq,
			fmt.Sprintf(`{"time":{"created":%d},"model":{"id":"model"}}`, seq))
	}
	db.Close()

	store := openStore(t, path)
	defer store.Close()
	if _, err := store.ReadOccupancy(context.Background(), "session"); !errors.Is(err, ErrNoOccupancy) {
		t.Fatalf("ReadOccupancy() error = %v, want bounded scan to return ErrNoOccupancy", err)
	}
}

func TestReadOccupancySkipsFailedTurnZero(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{ID: "session", Directory: "/work", Title: "Failed turn"})
	insertMessage(t, db, "previous", "session", "assistant", 1,
		`{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":120000,"cache":{"read":9000,"write":0}}}`)
	insertMessage(t, db, "failed", "session", "assistant", 2,
		`{"time":{"completed":2},"model":{"id":"model"},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`)
	db.Close()

	store := openStore(t, path)
	defer store.Close()
	got, err := store.ReadOccupancy(context.Background(), "session")
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got.Occupancy != 129_000 {
		t.Fatalf("occupancy = %d, want previous completed usage", got.Occupancy)
	}
}

func TestReadOccupancyRejectsInvalidLatestPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "malformed JSON", data: `{`},
		{name: "missing cache write", data: `{"time":{"completed":1},"tokens":{"input":1,"cache":{"read":2}}}`},
		{name: "negative field", data: `{"time":{"completed":1},"tokens":{"input":-1,"cache":{"read":2,"write":3}}}`},
		{name: "sum overflow", data: `{"time":{"completed":1},"tokens":{"input":9223372036854775807,"cache":{"read":1,"write":0}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := newTestDatabase(t)
			db := openFixtureDatabase(t, path)
			insertSession(t, db, testSession{ID: "session", Directory: "/work", Title: "Invalid"})
			insertMessage(t, db, "message", "session", "assistant", 1, tt.data)
			db.Close()

			store := openStore(t, path)
			defer store.Close()
			if _, err := store.ReadOccupancy(context.Background(), "session"); !errors.Is(err, ErrNoOccupancy) {
				t.Fatalf("ReadOccupancy() error = %v, want ErrNoOccupancy", err)
			}
		})
	}
}

func TestOpenIsReadOnly(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	store := openStore(t, path)
	defer store.Close()
	if _, err := store.db.ExecContext(context.Background(), `INSERT INTO session_v2 (id, directory, title, time_updated) VALUES ('write', '/work', 'write', 1)`); err == nil {
		t.Fatal("read-only store accepted a write")
	}
}

func TestOpenMissingStoreIsSemantic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "not-written.db")
	if _, err := Open(context.Background(), path); !errors.Is(err, ErrStoreNotReady) {
		t.Fatalf("Open() error = %v, want ErrStoreNotReady", err)
	}
}

func TestOpenUninitialisedStoreIsSemantic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "opencode2.db")
	db := openFixtureDatabase(t, path)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path); !errors.Is(err, ErrStoreNotReady) {
		t.Fatalf("Open() error = %v, want ErrStoreNotReady", err)
	}
}

func TestOpenDirectoryIsTransient(t *testing.T) {
	t.Parallel()

	if _, err := Open(context.Background(), t.TempDir()); err == nil || errors.Is(err, ErrStoreNotReady) {
		t.Fatalf("Open() error = %v, want transient database failure", err)
	}
}

func TestDatabasePathUsesExplicitOrXDGDataHome(t *testing.T) {
	if got, err := databasePath("/explicit/opencode2.db"); err != nil || got != "/explicit/opencode2.db" {
		t.Fatalf("databasePath(explicit) = %q, %v", got, err)
	}
	t.Setenv("XDG_DATA_HOME", "/xdg-data")
	got, err := databasePath("")
	if err != nil || got != filepath.Join("/xdg-data", "opencode", "opencode2.db") {
		t.Fatalf("databasePath(XDG) = %q, %v", got, err)
	}
}

func newTestDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode2.db")
	db := openFixtureDatabase(t, path)
	statements := []string{
		`CREATE TABLE session_v2 (
			id TEXT PRIMARY KEY,
			directory TEXT NOT NULL,
			title TEXT,
			parent_id TEXT,
			tokens_input INTEGER DEFAULT 0 NOT NULL,
			tokens_cache_read INTEGER DEFAULT 0 NOT NULL,
			tokens_cache_write INTEGER DEFAULT 0 NOT NULL,
			time_updated INTEGER NOT NULL
		)`,
		`CREATE TABLE session_message (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			type TEXT NOT NULL,
			seq INTEGER NOT NULL,
			data TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func openFixtureDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	return db
}

type testSession struct {
	ID               string
	Directory        string
	Title            string
	ParentID         string
	TokensInput      int64
	TokensCacheRead  int64
	TokensCacheWrite int64
	Updated          int64
}

func insertSession(t *testing.T, db *sql.DB, session testSession) {
	t.Helper()
	var parent any
	if session.ParentID != "" {
		parent = session.ParentID
	}
	if _, err := db.Exec(`INSERT INTO session_v2
		(id, directory, title, parent_id, tokens_input, tokens_cache_read, tokens_cache_write, time_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, session.ID, session.Directory, session.Title, parent,
		session.TokensInput, session.TokensCacheRead, session.TokensCacheWrite, session.Updated); err != nil {
		t.Fatal(err)
	}
}

func insertMessage(t *testing.T, db *sql.DB, id, sessionID, messageType string, seq int64, data string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO session_message (id, session_id, type, seq, data) VALUES (?, ?, ?, ?, ?)`, id, sessionID, messageType, seq, data); err != nil {
		t.Fatal(fmt.Errorf("insert message: %w", err))
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
