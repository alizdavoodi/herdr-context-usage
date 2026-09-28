package opencode2

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"

	_ "modernc.org/sqlite"
)

// Provenance for the fixtures in this file.
//
// Assistant rows and session titles are taken from the live opencode2 store,
// named per test with their session id and seq. Sessions there are 56 root
// sessions, 6 of them untitled and 6 with titles too long to fit a terminal
// title untruncated.
//
// Compaction rows are the one thing that cannot be taken from the store: it
// contains 1,254 session_message rows and not one of type 'compaction',
// because this installation has never compacted an opencode2 session. Their
// shape comes instead from the opencode2 binary itself
// (opencode2 0.0.0-next-17444), whose own query for the boundary is
//
//	select({seq}).from(session_message).where(and(
//	  eq(session_id, ...), eq(type, "compaction"),
//	  sql`json_extract(data, '$.status') = 'completed'`))
//	  .orderBy(desc(seq)).limit(1)
//
// which is where both the row type and the status predicate this provider
// matches on come from. The statuses other than "completed" are inferred from
// that predicate rather than observed.
//
// The title query and its refusals are pinned in opencodeshared; this first
// test pins it against the v2 schema, whose root sessions live in session_v2
// and whose child sessions carry the parent_id the query excludes them by.
func TestResolveTitleReadsTheV2SessionTable(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{
		ID: "target", Directory: "/work/acme", Title: "Probe test response", Updated: 100,
	})
	insertSession(t, db, testSession{
		ID: "child", Directory: "/work/acme", Title: "Probe test response", ParentID: "target", Updated: 500,
	})
	db.Close()

	s := openStore(t, path)
	defer s.Close()

	got, err := opencodeshared.ResolveTitle(context.Background(), s.DB, "session_v2", "/work/acme", "OC | Probe test response")
	if err != nil {
		t.Fatalf("ResolveTitle() error = %v", err)
	}
	if got != "target" {
		t.Fatalf("ResolveTitle() = %q, want target", got)
	}
}

func TestReadOccupancyUsesLatestAssistantPayload(t *testing.T) {
	t.Parallel()

	// msg_00c36acf0001WXPsa02H1XQ8Hi at seq 1325 and
	// msg_00c36c148001alHfsqgUb2Wdg4 at seq 1331 of session
	// ses_ff4059ac8ffe7NvtWxFA4cwGWo in the live opencode2 store: consecutive
	// completed turns where input is two orders of magnitude smaller than
	// cache.read, so a reading that ignored the cache fields would report 185
	// tokens for a 234k context.
	//
	// session_v2's own tokens_* columns are set to something impossible on
	// purpose. They look exactly like what is wanted and are cumulative
	// accumulators that keep rising across a compaction, so anything that reads
	// them instead of the messages fails here.
	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{
		ID: "session", Directory: "/work/acme", Title: "Measured",
		TokensInput: 900_000, TokensCacheRead: 800_000, TokensCacheWrite: 700_000,
	})
	insertMessage(t, db, "old-assistant", "session", "assistant", 1325,
		`{"time":{"completed":1786911310146},"model":{"id":"deepseek-v4-pro"},"tokens":{"input":92,"output":58,"cache":{"read":233728,"write":0}}}`)
	insertMessage(t, db, "newer-user", "session", "user", 1333,
		`{"role":"assistant","model":{"id":"wrong-role"},"tokens":{"input":999999,"cache":{"read":999999,"write":999999}}}`)
	insertMessage(t, db, "current-assistant", "session", "assistant", 1331,
		`{"time":{"completed":1786911314014},"model":{"id":"deepseek-v4-pro"},"tokens":{"input":185,"output":100,"cache":{"read":233984,"write":0}}}`)
	db.Close()

	s := openStore(t, path)
	defer s.Close()
	got, err := s.readOccupancy(context.Background(), "session")
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
	}
	if got.Occupancy != 234_169 {
		t.Fatalf("occupancy = %d, want input + cache read + cache write = 234169", got.Occupancy)
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
		`{"time":{"created":1786808200629},"agent":"build","model":{"id":"glm-5.2","providerID":"ollama-cloud","variant":"max"},"content":[{"type":"reasoning","text":"redacted"}]}`)
	insertMessage(t, db, "failed", "session", "assistant", 3,
		`{"time":{"completed":3},"model":{"id":"model"},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`)
	insertMessage(t, db, "malformed", "session", "assistant", 4, `{`)
	db.Close()

	s := openStore(t, path)
	defer s.Close()
	got, err := s.readOccupancy(context.Background(), "session")
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
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

			s := openStore(t, path)
			defer s.Close()
			if _, err := s.readOccupancy(context.Background(), "session"); !errors.Is(err, tt.wantErr) {
				t.Fatalf("readOccupancy() error = %v, want %v instead of pre-compaction occupancy", err, tt.wantErr)
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

	s := openStore(t, path)
	defer s.Close()
	got, err := s.readOccupancy(context.Background(), "session")
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
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

	s := openStore(t, path)
	defer s.Close()
	if _, err := s.readOccupancy(context.Background(), "session"); !errors.Is(err, ErrNoOccupancy) {
		t.Fatalf("readOccupancy() error = %v, want bounded scan to return ErrNoOccupancy", err)
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

			s := openStore(t, path)
			defer s.Close()
			if _, err := s.readOccupancy(context.Background(), "session"); !errors.Is(err, ErrNoOccupancy) {
				t.Fatalf("readOccupancy() error = %v, want ErrNoOccupancy", err)
			}
		})
	}
}

func TestOpenIsReadOnly(t *testing.T) {
	t.Parallel()

	path := newTestDatabase(t)
	s := openStore(t, path)
	defer s.Close()
	if _, err := s.ExecContext(context.Background(), `INSERT INTO session_v2 (id, directory, title, time_updated) VALUES ('write', '/work', 'write', 1)`); err == nil {
		t.Fatal("read-only store accepted a write")
	}
}

func TestOpenMissingStoreIsSemantic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "not-written.db")
	if _, err := open(context.Background(), path, ""); !errors.Is(err, sqlitestore.ErrMissing) {
		t.Fatalf("open() error = %v, want sqlitestore.ErrMissing", err)
	}
}

func TestOpenUninitialisedStoreIsSemantic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "opencode2.db")
	db := openFixtureDatabase(t, path)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := open(context.Background(), path, ""); !errors.Is(err, sqlitestore.ErrSchema) {
		t.Fatalf("open() error = %v, want sqlitestore.ErrSchema", err)
	}
}

func TestOpenDirectoryIsTransient(t *testing.T) {
	t.Parallel()

	if _, err := open(context.Background(), t.TempDir(), ""); err == nil || errors.Is(err, sqlitestore.ErrMissing) || errors.Is(err, sqlitestore.ErrSchema) {
		t.Fatalf("open() error = %v, want transient database failure", err)
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

func openStore(t *testing.T, path string) *store {
	t.Helper()
	s, err := open(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
