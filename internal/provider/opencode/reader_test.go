package opencode

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

// Provenance for the fixtures in this file: session
// ses_31c1bfde3ffeCEDhd6W270unu4 of the live opencode v1 store, whose
// messages, oldest first, are
//
//	msg_ce86820f6001vq8ZqNZcLMdJpE  t=…664246  assistant  input 1  read 0      write 168969
//	msg_ce8685bb6001qP8D2cbu3XY4bl  t=…679286  user       summary {"diffs":[]}
//	msg_ce8685bc7001SiK5DcsX6T7Q34  t=…679303  assistant  summary true, mode "compaction", input 2 read 0 write 131757
//	msg_ce8693c60001WODzJWBJkpkKUz  t=…736800  user       summary {"diffs":[]}
//	msg_ce8693c67001uXhw9SKU1RFVDc  t=…736807  assistant  input 3  read 39016  write 3341
//
// which is one whole compaction: 168,970 tokens before it and 42,360 after.
// The compaction row is the assistant message that wrote the summary, and its
// own 131,759 tokens are what it cost to summarise the old context, not the
// size of the new one — so it is a boundary to stop at rather than a reading.
//
// The two encodings of `summary` are not two encodings of one thing. Across
// the whole store, `summary: true` appears on 233 rows, every one an assistant
// message with mode and agent "compaction"; `summary: {…}` appears on 4,910
// rows, every one a user message carrying {"diffs":[]} or
// {"title":…,"diffs":[]}. The first is the compaction boundary. The second is
// ordinary prompt metadata that this provider also treats as one.
func TestReadOccupancyReadsV1TablesFromReadOnlyDatabase(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_current')`)
		// session_v2 in the v1 database is a stalled migration whose token columns
		// are cumulative and rise across a compaction. Set impossibly high here so
		// that reading them instead of the messages fails.
		exec(t, database, `INSERT INTO session_v2 (id, tokens_input) VALUES ('ses_current', 999999)`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce86820f6001vq8ZqNZcLMdJpE', 'ses_current', 1773425664246, '{"role":"assistant","mode":"Atlas (Plan Executor)","time":{"created":1773425650000,"completed":1773425679285},"tokens":{"input":1,"cache":{"read":0,"write":168969}}}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce8693c60001WODzJWBJkpkKUz', 'ses_current', 1773425736800, '{"role":"user","time":{"created":1773425736800},"summary":{"diffs":[]},"agent":"build"}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce8693c67001uXhw9SKU1RFVDc', 'ses_current', 1773425736807, '{"role":"assistant","mode":"Atlas (Plan Executor)","time":{"created":1773425736807,"completed":1773425740912},"tokens":{"input":3,"cache":{"read":39016,"write":3341}}}')`)
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

	s := openStore(t, databasePath)
	defer s.Close()
	got, err := s.readOccupancy(context.Background(), "ses_current")
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
	}
	if got != 42_360 {
		t.Fatalf("readOccupancy() = %d, want the newest completed turn's 3 + 39016 + 3341", got)
	}
}

func TestReadOccupancyTreatsZeroAsSemantic(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_zero')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_zero', 'ses_zero', 1, '{"role":"assistant","time":{"completed":1},"tokens":{"input":0,"cache":{"read":0,"write":0}}}')`)
	})

	s := openStore(t, databasePath)
	defer s.Close()
	if _, err := s.readOccupancy(context.Background(), "ses_zero"); !errors.Is(err, ErrNoOccupancy) {
		t.Fatalf("readOccupancy() error = %v, want ErrNoOccupancy", err)
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

		s := openStore(t, databasePath)
		got, err := s.readOccupancy(context.Background(), "ses_pending")
		s.Close()
		if err != nil || got != 92_481 {
			t.Fatalf("readOccupancy() = %d, %v; want prior completed occupancy", got, err)
		}
	}
}

func TestReadOccupancyDoesNotWalkBackAcrossLatestCompactionSummary(t *testing.T) {
	t.Parallel()

	// The first three messages of the session above, ending at the compaction
	// summary: 168,970 tokens are recorded before it and the next real reading,
	// 42,360, does not exist yet. Both numbers would be wrong to display, which
	// is why the instant has its own state rather than a number.
	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_compacted')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce86820f6001vq8ZqNZcLMdJpE', 'ses_compacted', 1773425664246, '{"role":"assistant","mode":"Atlas (Plan Executor)","time":{"created":1773425650000,"completed":1773425679285},"tokens":{"input":1,"cache":{"read":0,"write":168969}}}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce8685bb6001qP8D2cbu3XY4bl', 'ses_compacted', 1773425679286, '{"role":"user","time":{"created":1773425679286},"summary":{"diffs":[]},"agent":"build"}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce8685bc7001SiK5DcsX6T7Q34', 'ses_compacted', 1773425679303, '{"role":"assistant","mode":"compaction","agent":"compaction","summary":true,"time":{"created":1773425679303,"completed":1773425736799},"tokens":{"input":2,"cache":{"read":0,"write":131757}}}')`)
	})
	s := openStore(t, databasePath)
	defer s.Close()
	if _, err := s.readOccupancy(context.Background(), "ses_compacted"); !errors.Is(err, ErrJustCompacted) {
		t.Fatalf("readOccupancy() error = %v, want ErrJustCompacted", err)
	}
}

func TestReadOccupancyCrossesAnOrdinaryPromptSummary(t *testing.T) {
	t.Parallel()

	// The same three messages, one turn earlier: the pre-compaction reading, the
	// user's next prompt, and the assistant row that turn created before it has
	// completed. A prompt is not a boundary — the previous reading is still a
	// valid lower bound — and this is the state every v1 session is in for the
	// whole of every turn.
	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id) VALUES ('ses_running')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce86820f6001vq8ZqNZcLMdJpE', 'ses_running', 1773425664246, '{"role":"assistant","mode":"Atlas (Plan Executor)","time":{"created":1773425650000,"completed":1773425679285},"tokens":{"input":1,"cache":{"read":0,"write":168969}}}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce8685bb6001qP8D2cbu3XY4bl', 'ses_running', 1773425679286, '{"role":"user","time":{"created":1773425679286},"summary":{"diffs":[]},"agent":"build"}')`)
		exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_ce8685bc7001SiK5DcsX6T7Q34', 'ses_running', 1773425679303, '{"role":"assistant","mode":"Atlas (Plan Executor)","time":{"created":1773425679303}}')`)
	})
	s := openStore(t, databasePath)
	defer s.Close()
	got, err := s.readOccupancy(context.Background(), "ses_running")
	if err != nil {
		t.Fatalf("readOccupancy() error = %v, want the previous completed reading", err)
	}
	if got != 168_970 {
		t.Fatalf("readOccupancy() = %d, want the lower bound 1 + 0 + 168969", got)
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
		if _, err := open(context.Background(), databasePath, ""); !errors.Is(err, sqlitestore.ErrSchema) {
			t.Fatalf("open() error = %v, want sqlitestore.ErrSchema", err)
		}
	})

	t.Run("reported session no longer exists", func(t *testing.T) {
		databasePath := fixtureDatabase(t, func(database *sql.DB) {
			exec(t, database, `INSERT INTO session (id) VALUES ('ses_other')`)
		})
		s := openStore(t, databasePath)
		defer s.Close()
		if _, err := s.readOccupancy(context.Background(), "ses_current"); !errors.Is(err, ErrNoSession) {
			t.Fatalf("readOccupancy() error = %v, want ErrNoSession", err)
		}
	})
}

// The title query and its refusals are pinned in opencodeshared; this pins it
// against the v1 schema, whose root sessions live in session and whose child
// sessions carry the parent_id the query excludes them by.
func TestResolveTitleReadsTheV1SessionTable(t *testing.T) {
	t.Parallel()

	databasePath := fixtureDatabase(t, func(database *sql.DB) {
		exec(t, database, `INSERT INTO session (id, directory, title) VALUES ('target', '/work/acme', 'Probe test response')`)
		exec(t, database, `INSERT INTO session (id, directory, title, parent_id) VALUES ('child', '/work/acme', 'Probe test response', 'target')`)
	})
	s := openStore(t, databasePath)
	defer s.Close()

	id, err := opencodeshared.ResolveTitle(context.Background(), s.DB, "session", "/work/acme", "OC | Probe test response")
	if err != nil {
		t.Fatalf("ResolveTitle() error = %v", err)
	}
	if id != "target" {
		t.Fatalf("ResolveTitle() = %q, want target", id)
	}
}

func TestOpenMissingStoreIsSemantic(t *testing.T) {
	t.Parallel()

	if _, err := open(context.Background(), filepath.Join(t.TempDir(), "opencode.db"), ""); !errors.Is(err, sqlitestore.ErrMissing) {
		t.Fatalf("open() error = %v, want sqlitestore.ErrMissing", err)
	}
}

func TestOpenDirectoryIsTransient(t *testing.T) {
	t.Parallel()

	// Both opencode providers answer this the same way now: a directory where
	// the database should be is not a store that has not been written yet, so it
	// preserves the pane's value instead of blanking it.
	if _, err := open(context.Background(), t.TempDir(), ""); err == nil || errors.Is(err, sqlitestore.ErrMissing) || errors.Is(err, sqlitestore.ErrSchema) {
		t.Fatalf("open() error = %v, want a transient database failure", err)
	}
}

func TestOpenRejectsWrites(t *testing.T) {
	t.Parallel()

	s := openStore(t, fixtureDatabase(t, func(*sql.DB) {}))
	defer s.Close()
	if _, err := s.ExecContext(context.Background(), `INSERT INTO session (id) VALUES ('write')`); err == nil {
		t.Fatal("read-only opencode store accepted a write")
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

func openStore(t *testing.T, path string) *store {
	t.Helper()
	s, err := open(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
