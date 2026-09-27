package app

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	_ "modernc.org/sqlite"
)

// Provenance: 20260913_213233_86d843 and its 29,452 are a live Hermes Agent
// 0.21.5 session whose usage anchor matched agent.log's figure for its latest
// call. The provider's own tests carry the full fixture provenance.
const hermesSessionID = "20260913_213233_86d843"

// newAppHermesHome writes a state.db holding one session row, with only the
// columns the provider reads, and returns the home that contains it. The
// transcript is 42 active rows, the anchor's base_count plus one as on every
// anchored live session; compacted replaces it with 40 archived rows and 3
// active ones, the shape an in-place compaction leaves.
func newAppHermesHome(t *testing.T, modelConfig, endReason string, compacted bool) string {
	t.Helper()
	home := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(home, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	activeRows := 42
	if compacted {
		activeRows = 3
	}
	var endedAt, reason any
	if endReason != "" {
		endedAt, reason = 1757800000.0, endReason
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{query: `CREATE TABLE schema_version (version INTEGER NOT NULL)`},
		{query: `INSERT INTO schema_version VALUES (30)`},
		{query: `CREATE TABLE sessions (id TEXT PRIMARY KEY, model TEXT, model_config TEXT, ended_at REAL, end_reason TEXT)`},
		{query: `CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT NOT NULL, active INTEGER NOT NULL DEFAULT 1, compacted INTEGER NOT NULL DEFAULT 0)`},
		{query: `INSERT INTO sessions VALUES (?, 'claude-fable-5-1', ?, ?, ?)`, args: []any{hermesSessionID, modelConfig, endedAt, reason}},
		{query: `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?)
			INSERT INTO messages (session_id, active, compacted) SELECT ?, 1, 0 FROM n`, args: []any{activeRows, hermesSessionID}},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if compacted {
		if _, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 40)
			INSERT INTO messages (session_id, active, compacted) SELECT ?, 0, 1 FROM n`, hermesSessionID); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

const hermesAnchor = `{"_usage_anchor":{"prompt_tokens":29452,"completion_tokens":812,"base_count":41,"base_last_role":"tool","base_last_fp":"ab12"}}`

func hermesAgent(session *provider.AgentSession, tokens map[string]string) []herdr.Agent {
	return []herdr.Agent{{
		// ForegroundCWD is set and useless: this provider has no directory stage,
		// so a case that resolves must resolve from the reported id alone.
		Agent: "hermes", PaneID: "w1:p1", ForegroundCWD: "/work",
		AgentSession: session, Tokens: tokens,
	}}
}

func TestRefreshReportsHermesOccupancyFromAReportedID(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  hermesAgent(&provider.AgentSession{Kind: "id", Value: hermesSessionID}, nil),
		reports: make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{HermesHome: newAppHermesHome(t, hermesAnchor, "", false)}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"29k", "29452"} {
		t.Fatalf("report = %v, want the usage anchor's prompt tokens", got)
	}
	for _, want := range []string{`"provider":"hermes"`, `"context_state":"measured"`, `"occupancy":29452`, `"model_id":"claude-fable-5-1"`, `"resolution_mode":"agent_session"`} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("measurement = %s, want %s", log.String(), want)
		}
	}
}

func TestRefreshClearsSupersededHermesValues(t *testing.T) {
	t.Parallel()

	reported := &provider.AgentSession{Kind: "id", Value: hermesSessionID}
	tests := []struct {
		name      string
		session   *provider.AgentSession
		home      func(*testing.T) string
		wantState string
	}{
		{
			name:    "no agent session reported",
			session: nil,
			home:    func(t *testing.T) string { return newAppHermesHome(t, hermesAnchor, "", false) },
		},
		{
			// A desktop pane keeps reporting a session it closed days ago, and its
			// anchor is still a real number for that closed session.
			name:    "reported session has ended",
			session: reported,
			home:    func(t *testing.T) string { return newAppHermesHome(t, hermesAnchor, "cli_close", false) },
		},
		{
			name:      "compacted in place",
			session:   reported,
			home:      func(t *testing.T) string { return newAppHermesHome(t, `{}`, "", true) },
			wantState: `"context_state":"just_compacted"`,
		},
		{
			// The anchor clear Hermes runs after compacting is a separate write that
			// can fail silently, leaving the pre-compaction figure in place.
			name:      "anchor outlives an in-place compaction",
			session:   reported,
			home:      func(t *testing.T) string { return newAppHermesHome(t, hermesAnchor, "", true) },
			wantState: `"context_state":"just_compacted"`,
		},
		{
			name:      "rotated by compaction",
			session:   reported,
			home:      func(t *testing.T) string { return newAppHermesHome(t, hermesAnchor, "compression", false) },
			wantState: `"context_state":"just_compacted"`,
		},
		{
			name:    "store not written yet",
			session: reported,
			home:    func(t *testing.T) string { return t.TempDir() },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var log bytes.Buffer
			fake := &fakeHerdr{
				agents:  hermesAgent(tt.session, map[string]string{"ctx": "118k", "ctx_raw": "118019"}),
				reports: make(map[string][2]string),
			}
			if err := Refresh(context.Background(), fake, "", &log, Config{HermesHome: tt.home(t)}); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
				t.Fatalf("clears = %v, reports = %v; want the displayed value cleared", fake.clears, fake.reports)
			}
			if tt.wantState != "" && !strings.Contains(log.String(), tt.wantState) {
				t.Fatalf("measurement = %s, want %s", log.String(), tt.wantState)
			}
		})
	}
}

func TestRefreshPreservesHermesValueOnATransientReadFailure(t *testing.T) {
	t.Parallel()

	// A directory where the database should be is not evidence about the agent
	// session, so the displayed value must survive it.
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "state.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	fake := &fakeHerdr{
		agents:  hermesAgent(&provider.AgentSession{Kind: "id", Value: hermesSessionID}, map[string]string{"ctx": "29k", "ctx_raw": "29452"}),
		reports: make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", nil, Config{HermesHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 0 || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want a transient failure to preserve the value", fake.clears, fake.reports)
	}
}

// Sets the environment, so it cannot run in parallel. Unsetting HOME defeats
// os.UserHomeDir only on unix.
func TestRefreshClearsHermesRowWhenTheAgentHomeCannotBeResolved(t *testing.T) {
	t.Setenv("HOME", "")

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  hermesAgent(&provider.AgentSession{Kind: "id", Value: hermesSessionID}, map[string]string{"ctx": "29k", "ctx_raw": "29452"}),
		reports: make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want an unresolvable home to blank the row", fake.clears, fake.reports)
	}
	if got := log.String(); !strings.Contains(got, "agent home cannot be resolved") {
		t.Fatalf("measurement = %s, want the home named as the reason", got)
	}
}
