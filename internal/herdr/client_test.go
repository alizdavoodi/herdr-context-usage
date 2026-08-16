package herdr

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type recordedCall struct {
	name string
	args []string
}

type fakeRunner struct {
	responses [][]byte
	err       error
	calls     []recordedCall
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, recordedCall{name: name, args: append([]string(nil), args...)})
	if f.err != nil {
		return nil, f.err
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()

	client := NewClient("", nil)
	if client.binary != "herdr" {
		t.Fatalf("binary = %q, want herdr", client.binary)
	}
	if _, ok := client.runner.(execRunner); !ok {
		t.Fatalf("runner = %T, want execRunner", client.runner)
	}
}

func TestListAgents(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: [][]byte{[]byte(`{"result":{"agents":[{"agent":"claude","agent_status":"idle","foreground_cwd":"/work","pane_id":"w1:p1","tokens":{"ctx":"92k","ctx_raw":"92481"},"agent_session":{"source":"herdr:claude","agent":"claude","kind":"path","value":"/tmp/session.jsonl"}}]}}`)}}
	client := NewClient("/nix/store/herdr", runner)
	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(agents) != 1 || agents[0].PaneID != "w1:p1" || agents[0].AgentSession == nil || agents[0].AgentSession.Value != "/tmp/session.jsonl" {
		t.Fatalf("ListAgents() = %#v", agents)
	}
	if agents[0].Tokens["ctx_raw"] != "92481" {
		t.Fatalf("tokens = %#v", agents[0].Tokens)
	}
}

func TestProcessInfo(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: [][]byte{[]byte(`{"result":{"process_info":{"foreground_processes":[{"pid":42,"argv0":"claude","argv":["claude"]}]}}}`)}}
	client := NewClient("herdr", runner)
	processes, err := client.ProcessInfo(context.Background(), "w1:p1")
	if err != nil {
		t.Fatalf("ProcessInfo() error = %v", err)
	}
	if len(processes) != 1 || processes[0].PID != 42 || processes[0].Argv0 != "claude" {
		t.Fatalf("ProcessInfo() = %#v", processes)
	}
}

func TestReportAndClearUseOneStableSource(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: [][]byte{[]byte(`{"result":{}}`), []byte(`{"result":{}}`)}}
	client := NewClient("herdr", runner)
	if err := client.Report(context.Background(), "w1:p1", "92k", "92481"); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if err := client.Clear(context.Background(), "w1:p1"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	want := []recordedCall{
		{name: "herdr", args: []string{"pane", "report-metadata", "w1:p1", "--source", SourceID, "--token", "ctx=92k", "--token", "ctx_raw=92481"}},
		{name: "herdr", args: []string{"pane", "report-metadata", "w1:p1", "--source", SourceID, "--clear-token", "ctx", "--clear-token", "ctx_raw"}},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestClientErrors(t *testing.T) {
	t.Parallel()

	t.Run("command failure", func(t *testing.T) {
		runner := &fakeRunner{err: errors.New("boom")}
		client := NewClient("herdr", runner)
		if _, err := client.ListAgents(context.Background()); err == nil {
			t.Fatal("ListAgents() error = nil")
		}
		if _, err := client.ProcessInfo(context.Background(), "w1:p1"); err == nil {
			t.Fatal("ProcessInfo() error = nil")
		}
		if err := client.Report(context.Background(), "w1:p1", "1k", "1000"); err == nil {
			t.Fatal("Report() error = nil")
		}
		if err := client.Clear(context.Background(), "w1:p1"); err == nil {
			t.Fatal("Clear() error = nil")
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		runner := &fakeRunner{responses: [][]byte{[]byte("{"), []byte("{")}}
		client := NewClient("herdr", runner)
		if _, err := client.ListAgents(context.Background()); err == nil {
			t.Fatal("ListAgents() error = nil")
		}
		if _, err := client.ProcessInfo(context.Background(), "w1:p1"); err == nil {
			t.Fatal("ProcessInfo() error = nil")
		}
	})

	t.Run("exec runner reports spawn failure", func(t *testing.T) {
		if _, err := (execRunner{}).Run(context.Background(), "herdr-context-usage-command-that-does-not-exist"); err == nil {
			t.Fatal("execRunner.Run() error = nil")
		}
	})
}
