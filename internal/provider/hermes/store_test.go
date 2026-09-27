package hermes

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"

	_ "modernc.org/sqlite"
)

// Provenance for the fixtures in this file. The ids, models and figures are
// rows of the live state.db on the machine this provider was written against
// (Hermes Agent 0.21.5, schema_version 30): 20260913_213233_86d843 is an open
// session whose anchor matched agent.log's `in=29452` for its latest call, and
// 20260913_213851_c89da2 one closed by cli_close with its anchor still in
// place. Only the columns this provider reads are created.
const (
	openID   = "20260913_213233_86d843"
	closedID = "20260913_213851_c89da2"
)

type fixtureSession struct {
	ID, Model, EndReason string
	// Config is written verbatim, and "NULL" writes SQL NULL.
	Config string
	Ended  bool
	// Active is the number of active transcript rows, zero for 42: the anchor's
	// base_count of 41 plus one, the shape every anchored live session had.
	Active int
	// Compacted adds 40 rows archived by an in-place compaction.
	Compacted bool
}

// insertMessages writes n rows in one statement; n must be at least one.
func insertMessages(t *testing.T, db *sql.DB, id string, n int, active, compacted bool) {
	t.Helper()
	if _, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
		INSERT INTO messages (session_id, active, compacted) SELECT ?, ?, ? FROM n`, n, id, active, compacted); err != nil {
		t.Fatal(err)
	}
}

func newStateDB(t *testing.T, sessions ...fixtureSession) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE schema_version (version INTEGER NOT NULL)`,
		`INSERT INTO schema_version VALUES (30)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, model TEXT, model_config TEXT, ended_at REAL, end_reason TEXT)`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT NOT NULL, active INTEGER NOT NULL DEFAULT 1, compacted INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range sessions {
		var config, endedAt, endReason any
		if s.Config != "NULL" {
			config = s.Config
		}
		if s.Ended {
			endedAt, endReason = 1757800000.0, s.EndReason
		}
		if _, err := db.Exec(`INSERT INTO sessions VALUES (?, ?, ?, ?, ?)`, s.ID, s.Model, config, endedAt, endReason); err != nil {
			t.Fatal(err)
		}
		active := s.Active
		if active == 0 {
			active = 42
		}
		insertMessages(t, db, s.ID, active, true, false)
		if s.Compacted {
			insertMessages(t, db, s.ID, 40, false, true)
		}
	}
	return path
}

func anchor(promptTokens string) string {
	return `{"_usage_anchor":{"prompt_tokens":` + promptTokens + `,"completion_tokens":812,"base_count":41,"base_last_role":"tool","base_last_fp":"ab12"}}`
}

func read(t *testing.T, path, id string) (provider.Reading, error) {
	t.Helper()
	s, err := open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	return s.readOccupancy(context.Background(), id)
}

func TestReadOccupancyReportsTheUsageAnchor(t *testing.T) {
	t.Parallel()

	path := newStateDB(t, fixtureSession{ID: openID, Model: "claude-fable-5-1", Config: anchor("29452")})
	got, err := read(t, path, openID)
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
	}
	if got.Occupancy != 29452 || got.ModelID != "claude-fable-5-1" {
		t.Fatalf("readOccupancy() = %+v, want 29452 on claude-fable-5-1", got)
	}
	if got.TranscriptVersion == nil || *got.TranscriptVersion != 30 {
		t.Fatalf("TranscriptVersion = %v, want 30", got.TranscriptVersion)
	}
}

func TestReadOccupancyBlanksWhenItCannotStandBehindANumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		session fixtureSession
		want    error
	}{
		// An anchor still in place on a closed session is the trap: 118,019 is a
		// real figure, but not for whatever the pane now shows.
		{name: "closed session", session: fixtureSession{ID: closedID, Config: anchor("118019"), Ended: true, EndReason: "cli_close"}, want: ErrSessionEnded},
		{name: "rotated by compaction", session: fixtureSession{ID: closedID, Config: anchor("118019"), Ended: true, EndReason: "compression"}, want: ErrJustCompacted},
		{name: "compacted in place", session: fixtureSession{ID: openID, Config: `{}`, Compacted: true}, want: ErrJustCompacted},
		// Hermes commits the compacted transcript before it clears the anchor, and
		// swallows a failure to clear it, so the old figure can outlive the
		// transcript it priced.
		{name: "anchor outlives an in-place compaction", session: fixtureSession{ID: openID, Config: anchor("118019"), Active: 3, Compacted: true}, want: ErrJustCompacted},
		{name: "anchor without base_count", session: fixtureSession{ID: openID, Config: `{"_usage_anchor":{"prompt_tokens":29452}}`}, want: ErrNoOccupancy},
		{name: "anchor with zero base_count", session: fixtureSession{ID: openID, Config: `{"_usage_anchor":{"prompt_tokens":29452,"base_count":0}}`}, want: ErrNoOccupancy},
		{name: "no call completed yet", session: fixtureSession{ID: openID, Config: `{}`}, want: ErrNoOccupancy},
		{name: "anchor cleared to null", session: fixtureSession{ID: openID, Config: `{"_usage_anchor":null}`}, want: ErrNoOccupancy},
		{name: "null model_config", session: fixtureSession{ID: openID, Config: "NULL"}, want: ErrNoOccupancy},
		{name: "invalid model_config", session: fixtureSession{ID: openID, Config: `{not json`}, want: ErrNoOccupancy},
		{name: "zero prompt tokens", session: fixtureSession{ID: openID, Config: anchor("0")}, want: ErrNoOccupancy},
		{name: "negative prompt tokens", session: fixtureSession{ID: openID, Config: anchor("-5")}, want: ErrNoOccupancy},
		{name: "string prompt tokens", session: fixtureSession{ID: openID, Config: anchor(`"29452"`)}, want: ErrNoOccupancy},
		{name: "float prompt tokens", session: fixtureSession{ID: openID, Config: anchor("29452.5")}, want: ErrNoOccupancy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := read(t, newStateDB(t, tt.session), tt.session.ID)
			if !errors.Is(err, tt.want) {
				t.Fatalf("readOccupancy() = %+v, %v; want %v", got, err, tt.want)
			}
			// The failure path records the version too, so a blank caused by a
			// schema change is not indistinguishable from any other blank.
			if got.TranscriptVersion == nil || *got.TranscriptVersion != 30 {
				t.Fatalf("TranscriptVersion = %v on a failed read, want 30", got.TranscriptVersion)
			}
		})
	}
}

// Exactly base_count active rows is the transcript the anchor priced, and a
// session compacted earlier and re-anchored since must read normally.
func TestReadOccupancyReportsAnAnchorItsTranscriptStillCovers(t *testing.T) {
	t.Parallel()

	for name, session := range map[string]fixtureSession{
		"exactly base_count rows":      {ID: openID, Config: anchor("29452"), Active: 41},
		"re-anchored after compaction": {ID: openID, Config: anchor("29452"), Compacted: true},
	} {
		got, err := read(t, newStateDB(t, session), openID)
		if err != nil || got.Occupancy != 29452 {
			t.Fatalf("%s: readOccupancy() = %+v, %v; want 29452", name, got, err)
		}
	}
}

func TestReadOccupancyTreatsAnUnknownIDAsUnresolved(t *testing.T) {
	t.Parallel()

	path := newStateDB(t, fixtureSession{ID: openID, Config: anchor("29452")})
	if _, err := read(t, path, "20260101_000000_000000"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("readOccupancy() error = %v, want ErrNoSession", err)
	}
}

func TestOpenSeparatesAnUnwrittenStoreFromAnUnreadableOne(t *testing.T) {
	t.Parallel()

	if _, err := open(context.Background(), filepath.Join(t.TempDir(), "state.db")); !errors.Is(err, sqlitestore.ErrMissing) {
		t.Fatalf("open(missing) error = %v, want sqlitestore.ErrMissing", err)
	}

	empty := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", empty)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := open(context.Background(), empty); !errors.Is(err, sqlitestore.ErrSchema) {
		t.Fatalf("open(uninitialised) error = %v, want sqlitestore.ErrSchema", err)
	}

	_, err = open(context.Background(), t.TempDir())
	if err == nil || errors.Is(err, sqlitestore.ErrMissing) || errors.Is(err, sqlitestore.ErrSchema) {
		t.Fatalf("open(directory) error = %v, want a transient failure", err)
	}
}

// The read-only open itself is pinned in sqlitestore; this pins that hermes
// goes through it, since the store belongs to a running agent.
func TestOpenRefusesWrites(t *testing.T) {
	t.Parallel()

	s, err := open(context.Background(), newStateDB(t, fixtureSession{ID: openID, Config: anchor("29452")}))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`UPDATE sessions SET model_config = NULL`); err == nil {
		t.Fatal("a store opened for reading accepted a write against Hermes's live database")
	}
}

// Both refusals are answers about the pane, so neither may reach the store: the
// home here does not exist, and reaching it would answer sqlitestore.ErrMissing.
func TestResolveRefusesAnythingButAReportedID(t *testing.T) {
	t.Parallel()

	p := New(filepath.Join(t.TempDir(), "absent"))
	for name, session := range map[string]*provider.AgentSession{
		"no agent session": nil,
		"reported path":    {Kind: "path", Value: "/Users/me/.hermes/state.db"},
	} {
		reading, err := p.Measure(context.Background(), provider.Pane{ForegroundCWD: "/work", AgentSession: session})
		if !errors.Is(err, ErrNoSession) || reading.Mode != "" {
			t.Fatalf("%s: Measure() = %+v, %v; want ErrNoSession before the store", name, reading, err)
		}
	}

	reading, err := p.Measure(context.Background(), provider.Pane{AgentSession: &provider.AgentSession{Kind: "id", Value: openID}})
	if !errors.Is(err, sqlitestore.ErrMissing) || reading.Mode != provider.ResolutionAgentSession {
		t.Fatalf("Measure(id) = %+v, %v; want the store consulted under agent_session", reading, err)
	}
}
