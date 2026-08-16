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
	processCalls []string
	reports      map[string][2]string
	clears       []string
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

func TestRefreshFallsBackToEventPaneWhenPaneListFails(t *testing.T) {
	t.Parallel()

	want := errors.New("pane list failed")
	fake := &fakeHerdr{paneListErr: want}
	err := Refresh(context.Background(), fake, "w1:exited", nil, Config{})
	if !errors.Is(err, want) {
		t.Fatalf("Refresh() error = %v, want %v", err, want)
	}
	if !slices.Equal(fake.clears, []string{"w1:exited"}) {
		t.Fatalf("clears = %v, want event-pane fallback", fake.clears)
	}
}

func (f *fakeHerdr) ProcessInfo(_ context.Context, paneID string) ([]provider.Process, error) {
	f.processCalls = append(f.processCalls, paneID)
	return f.processes[paneID], nil
}

func (f *fakeHerdr) Report(_ context.Context, paneID, display, raw string) error {
	f.reports[paneID] = [2]string{display, raw}
	return nil
}

func (f *fakeHerdr) Clear(_ context.Context, paneID string) error {
	f.clears = append(f.clears, paneID)
	return nil
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
			{Agent: "codex", PaneID: "w1:p2", ForegroundCWD: "/work"},
		},
		processes: map[string][]provider.Process{
			"w1:p1": {{PID: 42, Argv0: "claude"}},
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
	wantClears := []string{"w1:exited", "w1:also-exited"}
	if !slices.Equal(fake.clears, wantClears) {
		t.Fatalf("clears = %v, want every stale occupancy pane %v", fake.clears, wantClears)
	}
}

func TestRefreshPreservesValueWhenClaudeSessionCannotBeResolved(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	home := t.TempDir()
	fake := &fakeHerdr{
		agents:    []herdr.Agent{{Agent: "claude", PaneID: "w1:p1", ForegroundCWD: "/ambiguous"}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want stale value preserved", fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"unknown"`) || !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("diagnostic = %s, want unknown state with null occupancy", got)
	}
}

func TestRefreshPreservesValueWhenTranscriptIsJustCompacted(t *testing.T) {
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
			AgentSession: &herdr.AgentSession{Kind: "path", Value: transcript},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want stale value preserved", fake.clears)
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
			AgentSession: &herdr.AgentSession{Kind: "path", Value: transcript},
		}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", &log, Config{ClaudeHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.reports) != 0 || len(fake.clears) != 0 {
		t.Fatalf("reports = %v, clears = %v; want no metadata mutation", fake.reports, fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"context_state":"unknown"`) || !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("diagnostic = %s, want unknown state with null occupancy", got)
	}
}
