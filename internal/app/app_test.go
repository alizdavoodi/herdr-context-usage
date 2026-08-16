package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

type fakeHerdr struct {
	agents       []herdr.Agent
	listErr      error
	processes    map[string][]provider.Process
	processCalls []string
	reports      map[string][2]string
	clears       []string
}

func (f *fakeHerdr) ListAgents(context.Context) ([]herdr.Agent, error) {
	return f.agents, f.listErr
}

func TestRefreshReturnsListError(t *testing.T) {
	t.Parallel()

	want := errors.New("list failed")
	if err := Refresh(context.Background(), &fakeHerdr{listErr: want}, "", nil); !errors.Is(err, want) {
		t.Fatalf("Refresh() error = %v, want %v", err, want)
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
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "w1:exited", nil); err != nil {
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
	if len(fake.clears) != 1 || fake.clears[0] != "w1:exited" {
		t.Fatalf("clears = %v, want exited event pane", fake.clears)
	}
}

func TestRefreshClearsUnknownClaudeMeasurement(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{
		agents:    []herdr.Agent{{Agent: "claude", PaneID: "w1:p1", ForegroundCWD: "/ambiguous"}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: "claude"}}},
		reports:   make(map[string][2]string),
	}
	if err := Refresh(context.Background(), fake, "", nil); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 1 || fake.clears[0] != "w1:p1" {
		t.Fatalf("clears = %v, want unresolved claude pane", fake.clears)
	}
}
