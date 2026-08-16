package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
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

func TestRefreshPreservesAgentValueWhenProcessInfoFails(t *testing.T) {
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
		t.Fatalf("clears = %v, want transient process failure to preserve value", fake.clears)
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
