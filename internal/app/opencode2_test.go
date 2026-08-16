package app

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	_ "modernc.org/sqlite"
)

// Provenance for the fixtures in this file. The session title and directory
// are the live opencode2 pane this plugin was verified against, which herdr
// reported as `OC | Probe test response`. The assistant payload shapes are the
// live store's, and the 2,445 / 14,848 pair is the measured turn where input
// is smaller than cache.read — the observation that settled that opencode2
// normalises cache out of input and that the three fields add. The compaction
// rows are the exception, described in the store's own tests: their shape
// comes from the opencode2 binary, because no session in this installation has
// ever compacted.
func TestRefreshReportsOpenCode2OccupancyResolvedFromTerminalTitle(t *testing.T) {
	t.Parallel()

	dbPath := newAppOpenCode2Database(t,
		[]appSession{{ID: "target", Directory: "/work/acme", Title: "Probe test response"}},
		[]appMessage{{ID: "answer", SessionID: "target", Type: "assistant", Seq: 1, Data: `{"time":{"completed":1},"model":{"id":"deepseek-v4-pro"},"tokens":{"input":2445,"cache":{"read":14848,"write":5}}}`}},
	)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent: "opencode", PaneID: "w1:p1", ForegroundCWD: "/work/acme",
			TerminalTitle: "OC | Probe test response",
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "/nix/store/bin/opencode2"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCode2DB: dbPath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"17k", "17298"} {
		t.Fatalf("report = %v, want opencode2 occupancy", got)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want successful measurement preserved", fake.clears)
	}
	for _, want := range []string{`"provider":"opencode2"`, `"context_state":"measured"`, `"occupancy":17298`, `"resolution_mode":"terminal_title"`} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("measurement = %s, want %s", log.String(), want)
		}
	}
}

func TestRefreshSkipsFailedOpenCode2TurnZero(t *testing.T) {
	t.Parallel()

	dbPath := newAppOpenCode2Database(t,
		[]appSession{{ID: "failed", Directory: "/work", Title: "Failed turn"}},
		[]appMessage{
			{ID: "previous", SessionID: "failed", Type: "assistant", Seq: 1, Data: `{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":120000,"cache":{"read":9000,"write":0}}}`},
			{ID: "failed-answer", SessionID: "failed", Type: "assistant", Seq: 2, Data: `{"time":{"completed":2},"model":{"id":"model"},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`},
		},
	)
	fake := &fakeHerdr{
		agents:    []herdr.Agent{{Agent: "opencode", PaneID: "w1:p1", ForegroundCWD: "/work", TerminalTitle: "OC | Failed turn"}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode2"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", nil, Config{OpenCode2DB: dbPath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"129k", "129000"} {
		t.Fatalf("report = %v, want previous completed usage", got)
	}
}

func TestRefreshKeepsOpenCode2OccupancyDuringInFlightTurn(t *testing.T) {
	t.Parallel()

	dbPath := newAppOpenCode2Database(t,
		[]appSession{{ID: "active", Directory: "/work", Title: "Active turn"}},
		[]appMessage{
			{ID: "previous", SessionID: "active", Type: "assistant", Seq: 1, Data: `{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":120000,"cache":{"read":9000,"write":0}}}`},
			{ID: "in-flight", SessionID: "active", Type: "assistant", Seq: 2, Data: `{"time":{"created":2},"model":{"id":"model"}}`},
		},
	)
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent: "opencode", PaneID: "w1:p1", ForegroundCWD: "/work", TerminalTitle: "OC | Active turn",
			Tokens: map[string]string{"ctx": "129k", "ctx_raw": "129000"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode2"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", nil, Config{OpenCode2DB: dbPath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"129k", "129000"} {
		t.Fatalf("report = %v, want previous completed lower bound", got)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want in-flight turn to preserve occupancy", fake.clears)
	}
}

func TestRefreshClearsOpenCode2OccupancyAtCompactionBoundary(t *testing.T) {
	t.Parallel()

	dbPath := newAppOpenCode2Database(t,
		[]appSession{{ID: "compacted", Directory: "/work", Title: "Compacted"}},
		[]appMessage{
			{ID: "pre-compaction", SessionID: "compacted", Type: "assistant", Seq: 1, Data: `{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":225000,"cache":{"read":9000,"write":0}}}`},
			{ID: "compaction", SessionID: "compacted", Type: "compaction", Seq: 2, Data: `{"status":"completed","reason":"auto","summary":"summary","recent":"recent"}`},
			{ID: "in-flight", SessionID: "compacted", Type: "assistant", Seq: 3, Data: `{"time":{"created":3},"model":{"id":"model"}}`},
		},
	)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent: "opencode", PaneID: "w1:p1", ForegroundCWD: "/work", TerminalTitle: "OC | Compacted",
			Tokens: map[string]string{"ctx": "234k", "ctx_raw": "234000"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode2"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCode2DB: dbPath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want pre-compaction occupancy cleared", fake.clears, fake.reports)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"just_compacted"`) {
		t.Fatalf("measurement = %q, want just_compacted state", got)
	}
}

func TestRefreshClearsSupersededOpenCode2Values(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		terminalTitle string
		database      func(*testing.T) string
	}{
		{
			name:          "untitled session",
			terminalTitle: "OpenCode",
			database: func(t *testing.T) string {
				return newAppOpenCode2Database(t, nil, nil)
			},
		},
		{
			name:          "unresolved session",
			terminalTitle: "OC | Missing",
			database: func(t *testing.T) string {
				return newAppOpenCode2Database(t, []appSession{{ID: "other", Directory: "/work", Title: "Other"}}, nil)
			},
		},
		{
			name:          "resolved session has no occupancy yet",
			terminalTitle: "OC | Waiting",
			database: func(t *testing.T) string {
				return newAppOpenCode2Database(t, []appSession{{ID: "waiting", Directory: "/work", Title: "Waiting"}}, nil)
			},
		},
		{
			name:          "store not written yet",
			terminalTitle: "OC | Waiting",
			database: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "opencode2.db")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeHerdr{
				agents: []herdr.Agent{{
					Agent: "opencode", PaneID: "w1:p1", ForegroundCWD: "/work",
					TerminalTitle: tt.terminalTitle, Tokens: map[string]string{"ctx_raw": "92481"},
				}},
				processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode2"}}},
				reports:   make(map[string][2]string),
			}
			if err := Refresh(context.Background(), fake, "", nil, Config{OpenCode2DB: tt.database(t)}); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
				t.Fatalf("clears = %v, reports = %v; want superseded value cleared", fake.clears, fake.reports)
			}
		})
	}
}

// A blank opencode row is only useful if the plugin can say why it is blank.
// These two reasons are invisible from the store: a plugin route's title looks
// like a session title, and a suppressed title looks like no agent at all.
func TestRefreshExplainsAnOpenCodeTitleThatNamesNoSession(t *testing.T) {
	t.Parallel()

	dbPath := newAppOpenCode2Database(t, []appSession{{ID: "target", Directory: "/work", Title: "Probe test response"}}, nil)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent: "opencode", PaneID: "w1:p1", ForegroundCWD: "/work",
			// A plugin route interpolates its plugin name here, and plugin titles
			// are never shortened, so one this long cannot be a session title.
			TerminalTitle: "OC | herdr-navigator plugin workspace inspector route",
			Tokens:        map[string]string{"ctx_raw": "92481"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode2"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCode2DB: dbPath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) {
		t.Fatalf("clears = %v, want a plugin route to blank the row", fake.clears)
	}
	if got := log.String(); !strings.Contains(got, "plugin route") {
		t.Fatalf("diagnostic = %s, want the plugin route named", got)
	}
}

func TestRefreshExplainsASuppressedOpenCodeTerminalTitle(t *testing.T) {
	// Not parallel: the variable is process-wide, and the point of the test is
	// that this process inherits it the way the herdr server would.
	t.Setenv("OPENCODE_DISABLE_TERMINAL_TITLE", "1")

	dbPath := newAppOpenCode2Database(t, []appSession{{ID: "target", Directory: "/work", Title: "Probe test response"}}, nil)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent: "opencode", PaneID: "w1:p1", ForegroundCWD: "/work",
			TerminalTitle: "zsh", Tokens: map[string]string{"ctx_raw": "92481"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode2"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCode2DB: dbPath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := log.String(); !strings.Contains(got, "OPENCODE_DISABLE_TERMINAL_TITLE") {
		t.Fatalf("diagnostic = %s, want the permanently blank row explained", got)
	}
}

func TestRefreshPreservesOpenCode2ValueOnTransientDatabaseFailure(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent: "opencode", PaneID: "w1:p1", ForegroundCWD: "/work",
			TerminalTitle: "OC | Session", Tokens: map[string]string{"ctx_raw": "92481"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode2"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", nil, Config{OpenCode2DB: t.TempDir()}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 0 || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want transient failure to preserve current value", fake.clears, fake.reports)
	}
}

type appSession struct {
	ID        string
	Directory string
	Title     string
}

type appMessage struct {
	ID        string
	SessionID string
	Type      string
	Seq       int64
	Data      string
}

func newAppOpenCode2Database(t *testing.T, sessions []appSession, messages []appMessage) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode2.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, directory TEXT NOT NULL, title TEXT, parent_id TEXT, time_updated INTEGER NOT NULL)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, type TEXT NOT NULL, seq INTEGER NOT NULL, data TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range sessions {
		if _, err := db.Exec(`INSERT INTO session_v2 (id, directory, title, time_updated) VALUES (?, ?, ?, 1)`, session.ID, session.Directory, session.Title); err != nil {
			t.Fatal(err)
		}
	}
	for _, message := range messages {
		if _, err := db.Exec(`INSERT INTO session_message (id, session_id, type, seq, data) VALUES (?, ?, ?, ?, ?)`, message.ID, message.SessionID, message.Type, message.Seq, message.Data); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
