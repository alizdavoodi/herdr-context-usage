package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"

	_ "modernc.org/sqlite"
)

type fakeHerdr struct {
	agents       []herdr.Agent
	listErr      error
	panes        []herdr.Pane
	paneListErr  error
	processes    map[string][]provider.Process
	processErrs  map[string]error
	processCalls []string
	reports      map[string][2]string
	clears       []string
	clearErrs    map[string]error
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (f *fakeHerdr) ListAgents(context.Context) ([]herdr.Agent, error) {
	return f.agents, f.listErr
}

func (f *fakeHerdr) ListPanes(context.Context) ([]herdr.Pane, error) {
	return f.panes, f.paneListErr
}

func TestRefreshReturnsListError(t *testing.T) {
	t.Parallel()

	want := errors.New("list failed")
	if err := Refresh(context.Background(), &fakeHerdr{listErr: want}, "", nil, Config{}); !errors.Is(err, want) {
		t.Fatalf("Refresh() error = %v, want %v", err, want)
	}
}

func TestRefreshDoesNotFailSuccessfulMeasurementsWhenPaneListFails(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	transcript := filepath.Join(home, "session.jsonl")
	entry := `{"type":"assistant","message":{"usage":{"input_tokens":840,"cache_read_input_tokens":90000,"cache_creation_input_tokens":1641}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeHerdr{
		agents:      []herdr.Agent{{Agent: "claude", PaneID: "w1:p1", AgentSession: &herdr.AgentSession{Kind: "path", Value: transcript}}},
		paneListErr: errors.New("pane list failed"),
		processes:   map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:     make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "w1:exited", nil, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v, want successful measurement", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:exited"}) {
		t.Fatalf("clears = %v, want event-pane fallback", fake.clears)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want successful measurement", got)
	}
}

func (f *fakeHerdr) ProcessInfo(_ context.Context, paneID string) ([]provider.Process, error) {
	f.processCalls = append(f.processCalls, paneID)
	return f.processes[paneID], f.processErrs[paneID]
}

func (f *fakeHerdr) Report(_ context.Context, paneID, display, raw string) error {
	f.reports[paneID] = [2]string{display, raw}
	return nil
}

func (f *fakeHerdr) Clear(_ context.Context, paneID string) error {
	f.clears = append(f.clears, paneID)
	return f.clearErrs[paneID]
}

func TestRefreshFansOutAndClearsEventPaneAfterAgentExit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	transcript := filepath.Join(root, "session.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"assistant","message":{"usage":{"input_tokens":840,"cache_read_input_tokens":90000,"cache_creation_input_tokens":1641}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeHerdr{
		agents: []herdr.Agent{
			{Agent: "claude", PaneID: "w1:p1", ForegroundCWD: "/work", AgentSession: &herdr.AgentSession{Kind: "path", Value: transcript}},
			{Agent: "codex", PaneID: "w1:p2", ForegroundCWD: "/work", Tokens: map[string]string{"ctx_raw": "70000"}},
		},
		processes: map[string][]provider.Process{
			"w1:p1": {{PID: 41, Argv0: "codex"}, {PID: 42, Argv0: "claude"}},
			"w1:p2": {{PID: 43, Argv0: "codex"}},
		},
		panes: []herdr.Pane{
			{PaneID: "w1:p1", Tokens: map[string]string{"ctx": "92k", "ctx_raw": "92481"}},
			{PaneID: "w1:exited", Tokens: map[string]string{"ctx": "80k", "ctx_raw": "80000"}},
			{PaneID: "w1:also-exited", Tokens: map[string]string{"ctx_raw": "70000"}},
			{PaneID: "w1:shell"},
		},
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "w1:exited", nil, Config{ClaudeHome: root}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.processCalls) != 2 {
		t.Fatalf("ProcessInfo called for %v, want every live agent", fake.processCalls)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("claude report = %v", got)
	}
	if _, ok := fake.reports["w1:p2"]; ok {
		t.Fatal("codex must remain unimplemented")
	}
	wantClears := []string{"w1:exited", "w1:also-exited", "w1:p2"}
	if !slices.Equal(fake.clears, wantClears) {
		t.Fatalf("clears = %v, want every stale occupancy pane %v", fake.clears, wantClears)
	}
}

func TestRefreshPreservesValueWhenProcessInfoFails(t *testing.T) {
	t.Parallel()

	processErr := errors.New("process info failed")
	fake := &fakeHerdr{
		agents:      []herdr.Agent{{Agent: "opencode", PaneID: "w1:p1", Tokens: map[string]string{"ctx_raw": "92481"}}},
		processErrs: map[string]error{"w1:p1": processErr},
	}
	err := Refresh(context.Background(), fake, "", nil, Config{})
	if !errors.Is(err, processErr) {
		t.Fatalf("Refresh() error = %v, want %v", err, processErr)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want transient process failure to preserve the value", fake.clears)
	}
}

func TestRefreshClearsValueWhenClaudeSessionCannotBeResolved(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	home := t.TempDir()
	fake := &fakeHerdr{
		agents:    []herdr.Agent{{Agent: "claude", PaneID: "w1:p1", ForegroundCWD: "/ambiguous", Tokens: map[string]string{"ctx_raw": "92481"}}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) {
		t.Fatalf("clears = %v, want unresolved session cleared", fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"type":"measurement"`) || !strings.Contains(got, `"context_state":"unknown"`) || !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("diagnostic = %s, want unknown state with null occupancy", got)
	}
}

func TestRefreshReportsCodexPostCompactionOccupancy(t *testing.T) {
	t.Parallel()

	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	entries := "" +
		`{"type":"event_msg","payload":{"info":{"last_token_usage":{"input_tokens":245400,"cached_input_tokens":238336,"total_tokens":245715}}}}` + "\n" +
		`{"type":"compacted","payload":{"message":"","replacement_history":[]}}` + "\n" +
		`{"type":"event_msg","payload":{"info":{"last_token_usage":{"input_tokens":0,"cached_input_tokens":0,"total_tokens":18162},"model_context_window":272000}}}` + "\n"
	if err := os.WriteFile(rollout, []byte(entries), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "codex",
			PaneID:       "w1:p1",
			AgentSession: &herdr.AgentSession{Kind: "path", Value: rollout},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "codex"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"18k", "18162"} {
		t.Fatalf("report = %v, want post-compaction absolute occupancy", got)
	}
	if got := log.String(); !strings.Contains(got, `"provider":"codex"`) || !strings.Contains(got, `"resolution_mode":"agent_session"`) {
		t.Fatalf("measurement = %s, want codex agent_session resolution", got)
	}
}

func TestRefreshPreservesCodexValueAfterATurnThatRecordedNoUsage(t *testing.T) {
	t.Parallel()

	// Codex reads the newest usage row rather than walking back, so a turn that
	// recorded nothing has to leave the previous reading standing rather than
	// blank the pane. No rollout in the local corpus has produced this row.
	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	entries := "" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":85751,"cached_input_tokens":84736,"total_tokens":86542}}}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":0,"cached_input_tokens":0,"total_tokens":0}}}}` + "\n"
	if err := os.WriteFile(rollout, []byte(entries), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "codex",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx": "86k", "ctx_raw": "86542"},
			AgentSession: &herdr.AgentSession{Kind: "path", Value: rollout},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "codex"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 0 || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want the previous reading left standing", fake.clears, fake.reports)
	}
}

func TestRefreshClearsValueWhenCodexRolloutIsNotWritten(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "not-written.jsonl")
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "codex",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "path", Value: missing},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "codex"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) {
		t.Fatalf("clears = %v, want previous occupancy cleared", fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"provider":"codex"`) || !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("measurement = %s, want unknown Codex occupancy", got)
	}
}

func TestRefreshClearsValueWhenTranscriptIsJustCompacted(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	transcript := filepath.Join(home, "session.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"summary","isCompactSummary":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "claude",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "path", Value: transcript},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) {
		t.Fatalf("clears = %v, want pre-compaction value cleared", fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"just_compacted"`) || !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("diagnostic = %s, want just-compacted state with null occupancy", got)
	}
}

func TestRefreshTreatsSyntheticZeroUsageAsUnknown(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	transcript := filepath.Join(home, "session.jsonl")
	entry := `{"type":"assistant","message":{"usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "claude",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "path", Value: transcript},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.reports) != 0 || !slices.Equal(fake.clears, []string{"w1:p1"}) {
		t.Fatalf("reports = %v, clears = %v; want empty transcript cleared", fake.reports, fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"unknown"`) || !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("diagnostic = %s, want unknown state with null occupancy", got)
	}
}

func TestRefreshClearsValueWhenResolvedTranscriptDoesNotExist(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	missing := filepath.Join(home, "not-created-yet.jsonl")
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "claude",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "path", Value: missing},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) {
		t.Fatalf("clears = %v, want previous session value cleared", fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"unknown"`) || !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("diagnostic = %s, want unknown state with null occupancy", got)
	}
}

func TestRefreshPreservesValueOnTranscriptIOFailure(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "claude",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "path", Value: home},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want I/O failure to preserve value", fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"unknown"`) || !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("diagnostic = %s, want unknown state with null occupancy", got)
	}
}

func TestRefreshReportsOccupancyWhenMeasurementLoggingFails(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	transcript := filepath.Join(home, "session.jsonl")
	entry := `{"type":"assistant","message":{"usage":{"input_tokens":840,"cache_read_input_tokens":90000,"cache_creation_input_tokens":1641}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	writeErr := errors.New("stdout failed")
	fake := &fakeHerdr{
		agents:    []herdr.Agent{{Agent: "claude", PaneID: "w1:p1", AgentSession: &herdr.AgentSession{Kind: "path", Value: transcript}}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	err := Refresh(context.Background(), fake, "", errorWriter{err: writeErr}, Config{ClaudeHome: home})
	if !errors.Is(err, writeErr) {
		t.Fatalf("Refresh() error = %v, want logging error %v", err, writeErr)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want occupancy despite logging failure", got)
	}
}

func TestRefreshReportsPaneListFailureWithoutMaskingMeasurement(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	transcript := filepath.Join(home, "session.jsonl")
	entry := `{"type":"assistant","message":{"usage":{"input_tokens":840,"cache_read_input_tokens":90000,"cache_creation_input_tokens":1641}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:      []herdr.Agent{{Agent: "claude", PaneID: "w1:p1", AgentSession: &herdr.AgentSession{Kind: "path", Value: transcript}}},
		paneListErr: errors.New("pane list failed"),
		processes:   map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:     make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v, want successful measurement", err)
	}
	if got := log.String(); !strings.Contains(got, `"type":"maintenance"`) || !strings.Contains(got, `"type":"measurement"`) || !strings.Contains(got, `"operation":"list_panes"`) || !strings.Contains(got, `"error":"pane list failed"`) {
		t.Fatalf("diagnostic = %s, want pane-list failure", got)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want successful measurement", got)
	}
}

func TestRefreshReportsEveryStaleMetadataClearFailure(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		panes: []herdr.Pane{
			{PaneID: "w1:p1", Tokens: map[string]string{"ctx": "92k"}},
			{PaneID: "w1:p2", Tokens: map[string]string{"ctx_raw": "92481"}},
		},
		clearErrs: map[string]error{
			"w1:p1": errors.New("first clear failed"),
			"w1:p2": errors.New("second clear failed"),
		},
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{}); err != nil {
		t.Fatalf("Refresh() error = %v, want sweep failures to preserve exit status", err)
	}
	got := log.String()
	for _, want := range []string{
		`"type":"maintenance"`,
		`"operation":"clear_stale_metadata"`,
		`"pane_id":"w1:p1"`,
		`"error":"first clear failed"`,
		`"pane_id":"w1:p2"`,
		`"error":"second clear failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic = %s, want %s", got, want)
		}
	}
}

func TestRefreshReportsEventPaneFallbackClearFailure(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		paneListErr: errors.New("pane list failed"),
		clearErrs:   map[string]error{"w1:exited": errors.New("fallback clear failed")},
	}
	if err := Refresh(context.Background(), fake, "w1:exited", &log, Config{}); err != nil {
		t.Fatalf("Refresh() error = %v, want sweep failures to preserve exit status", err)
	}
	got := log.String()
	for _, want := range []string{
		`"operation":"list_panes"`,
		`"operation":"clear_stale_metadata"`,
		`"pane_id":"w1:exited"`,
		`"error":"fallback clear failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic = %s, want %s", got, want)
		}
	}
}

func TestRefreshReportsOpenCodeV1Occupancy(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	createOpenCodeDatabase(t, filepath.Join(dataDir, "opencode.db"), "ses_current", `{"role":"assistant","time":{"completed":1},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}`)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "opencode",
			PaneID:       "w1:p1",
			AgentSession: &herdr.AgentSession{Kind: "id", Value: "ses_current"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCodeDB: filepath.Join(dataDir, "opencode.db")}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want opencode v1 occupancy", got)
	}
	if got := log.String(); !strings.Contains(got, `"provider":"opencode"`) || !strings.Contains(got, `"resolution_mode":"agent_session"`) {
		t.Fatalf("diagnostic = %s, want opencode agent_session measurement", got)
	}
}

func TestRefreshReportsOpenCodeV1OccupancyResolvedFromTerminalTitle(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	databasePath := filepath.Join(dataDir, "opencode.db")
	createOpenCodeDatabase(t, databasePath, "ses_current", `{"role":"assistant","time":{"completed":1},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}`)
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE session SET directory = ?, title = ? WHERE id = ?`, "/work/acme", "Probe test response", "ses_current"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:         "opencode",
			PaneID:        "w1:p1",
			ForegroundCWD: "/work/acme",
			TerminalTitle: "OC | Probe test response",
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCodeDB: databasePath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want opencode v1 occupancy", got)
	}
	if got := log.String(); !strings.Contains(got, `"resolution_mode":"terminal_title"`) {
		t.Fatalf("diagnostic = %s, want terminal_title resolution", got)
	}
}

func TestRefreshOpenCodeClearsValueWhenStoreIsMissing(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "opencode",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "id", Value: "ses_current"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCodeDB: filepath.Join(t.TempDir(), "opencode.db")}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) {
		t.Fatalf("clears = %v, want missing store to clear stale occupancy", fake.clears)
	}
	if !strings.Contains(log.String(), `"resolution_mode":"agent_session"`) {
		t.Fatalf("diagnostic = %s, want known agent_session mode", log.String())
	}
}

func TestRefreshOpenCodeClearsValueForZeroOccupancy(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	createOpenCodeDatabase(t, filepath.Join(dataDir, "opencode.db"), "ses_aborted", `{"role":"assistant","time":{"completed":1},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`)
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "opencode",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "id", Value: "ses_aborted"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", nil, Config{OpenCodeDB: filepath.Join(dataDir, "opencode.db")}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want zero occupancy to clear stale metadata", fake.clears, fake.reports)
	}
}

func TestRefreshOpenCodeUsesPreviousCompletedOccupancyAfterZeroTurn(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	databasePath := filepath.Join(dataDir, "opencode.db")
	createOpenCodeDatabase(t, databasePath, "ses_current", `{"role":"assistant","time":{"completed":1},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}`)
	appendOpenCodeMessage(t, databasePath, "msg_zero", "ses_current", 2, `{"role":"assistant","time":{"completed":2},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`)
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "opencode",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "id", Value: "ses_current"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", nil, Config{OpenCodeDB: databasePath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} || len(fake.clears) != 0 {
		t.Fatalf("report = %v, clears = %v; want previous completed lower bound", got, fake.clears)
	}
}

func TestRefreshOpenCodeClearsValueWhenNewestMessageIsCompactionSummary(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	databasePath := filepath.Join(dataDir, "opencode.db")
	createOpenCodeDatabase(t, databasePath, "ses_current", `{"role":"assistant","time":{"completed":1},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}`)
	// msg_ce8685bc7001SiK5DcsX6T7Q34 of session ses_31c1bfde3ffeCEDhd6W270unu4:
	// the assistant message that wrote a compaction summary. Its own token counts
	// are what summarising the old context cost, not the size of the new one.
	appendOpenCodeMessage(t, databasePath, "msg_summary", "ses_current", 2,
		`{"role":"assistant","mode":"compaction","agent":"compaction","summary":true,"time":{"created":1773425679303,"completed":1773425736799},"tokens":{"input":2,"cache":{"read":0,"write":131757}}}`)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "opencode",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "id", Value: "ses_current"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCodeDB: databasePath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want pre-compaction value cleared", fake.clears, fake.reports)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"just_compacted"`) {
		t.Fatalf("diagnostic = %s, want just-compacted state", got)
	}
}

func TestRefreshOpenCodeClearsWhenSessionCannotBeResolved(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	createOpenCodeDatabase(t, filepath.Join(dataDir, "opencode.db"), "ses_someone_else", `{"role":"assistant","time":{"completed":1},"tokens":{"input":999999,"cache":{"read":0,"write":0}}}`)
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:  "opencode",
			PaneID: "w1:p1",
			Tokens: map[string]string{"ctx_raw": "92481"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", nil, Config{OpenCodeDB: filepath.Join(dataDir, "opencode.db")}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want blank unresolved opencode row", fake.clears, fake.reports)
	}
}

func TestRefreshDoesNotClaimOpenCode2AsV1(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	createOpenCodeDatabase(t, filepath.Join(dataDir, "opencode.db"), "ses_current", `{"role":"assistant","time":{"completed":1},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}`)
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "opencode",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "92481"},
			AgentSession: &herdr.AgentSession{Kind: "id", Value: "ses_current"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: ".OpenCode2"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", nil, Config{OpenCodeDB: filepath.Join(dataDir, "opencode.db")}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.reports) != 0 {
		t.Fatalf("reports = %v, want opencode2 excluded from v1", fake.reports)
	}
}

// Each provider detects a compaction against its own store, and the three
// detections have nothing in common: codex writes a top-level marker record,
// opencode v1 sets a flag inside a user message, opencode2 uses a dedicated row
// type. These assertions are what keep all of them reporting the same state to
// herdr despite that, so they are repeated per provider rather than shared.
func TestRefreshClearsValueWhenCodexRolloutIsJustCompacted(t *testing.T) {
	t.Parallel()

	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	entries := "" +
		`{"type":"event_msg","payload":{"info":{"last_token_usage":{"input_tokens":245400,"cached_input_tokens":238336,"total_tokens":245715}}}}` + "\n" +
		`{"type":"compacted","payload":{"message":"","replacement_history":[]}}` + "\n"
	if err := os.WriteFile(rollout, []byte(entries), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:        "codex",
			PaneID:       "w1:p1",
			Tokens:       map[string]string{"ctx_raw": "245715"},
			AgentSession: &herdr.AgentSession{Kind: "path", Value: rollout},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "codex"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want pre-compaction value cleared", fake.clears, fake.reports)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"just_compacted"`) {
		t.Fatalf("diagnostic = %s, want just-compacted state", got)
	}
}

func TestRefreshClearsValueWhenOpenCode2SessionIsJustCompacted(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "opencode2.db")
	createOpenCode2Database(t, databasePath, "ses_current",
		`{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":225000,"cache":{"read":9000,"write":0}}}`)
	appendOpenCode2Message(t, databasePath, "compaction", "ses_current", "compaction", 2,
		`{"status":"completed","reason":"auto","summary":"summary","recent":"recent"}`)
	var log bytes.Buffer
	// opencode2 has no agent_session path: herdr reports no session for these
	// panes, so correlation runs off directory plus the TUI's terminal title.
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent:         "opencode",
			PaneID:        "w1:p1",
			ForegroundCWD: "/work",
			TerminalTitle: "OC | Test session",
			Tokens:        map[string]string{"ctx_raw": "234000"},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "opencode2"}}},
		reports:   make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OpenCode2DB: databasePath}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want pre-compaction value cleared", fake.clears, fake.reports)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"just_compacted"`) {
		t.Fatalf("diagnostic = %s, want just-compacted state", got)
	}
}

// Provenance for the fixtures in this file. The claude usage objects are the
// assistant records at lines 546 and 561 of
// ~/.claude/projects/-Users-alirezadavoodi-projects-github-com-alizdavoodi-herdr-context-usage/6eb1e90f-04f1-4fa7-b553-9142da3f7913.jsonl,
// and the codex records are lines 396, 397 and 400 of
// ~/.codex/sessions/2026/07/31/rollout-2026-07-31T17-43-45-019fb8d8-85b8-7c50-82ac-bc46ddf0d5fe.jsonl,
// both described in full in the two providers' own reader tests. The opencode
// v1 rows come from session ses_31c1bfde3ffeCEDhd6W270unu4, and the opencode2
// compaction rows are the one shape taken from a binary rather than a store,
// because no opencode2 session here has ever compacted. The numbers 840 /
// 90000 / 1641 that appear below are the design document's worked example,
// kept because several assertions read as the same 92,481 across providers.
func createOpenCode2Database(t *testing.T, path, sessionID, message string) {
	t.Helper()

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, query := range []string{
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
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(
		`INSERT INTO session_v2 (id, directory, title, time_updated) VALUES (?, '/work', 'Test session', 1)`,
		sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO session_message (id, session_id, type, seq, data) VALUES ('msg_current', ?, 'assistant', 1, ?)`,
		sessionID, message); err != nil {
		t.Fatal(err)
	}
}

func appendOpenCode2Message(t *testing.T, path, id, sessionID, messageType string, seq int64, data string) {
	t.Helper()

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(
		`INSERT INTO session_message (id, session_id, type, seq, data) VALUES (?, ?, ?, ?, ?)`,
		id, sessionID, messageType, seq, data); err != nil {
		t.Fatal(err)
	}
}

func createOpenCodeDatabase(t *testing.T, path, sessionID, message string) {
	t.Helper()

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, query := range []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, title TEXT, parent_id TEXT)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`,
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO session (id) VALUES (?)`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES ('msg_current', ?, 1, ?)`, sessionID, message); err != nil {
		t.Fatal(err)
	}
}

func appendOpenCodeMessage(t *testing.T, path, id, sessionID string, created int64, data string) {
	t.Helper()

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`, id, sessionID, created, data); err != nil {
		t.Fatal(err)
	}
}
