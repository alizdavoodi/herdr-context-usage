package herdr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

type recordedCall struct {
	name string
	args []string
}

func TestExecRunnerSeparatesStdoutAndStderr(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("success warning does not corrupt JSON", func(t *testing.T) {
		output, err := (execRunner{}).Run(context.Background(), executable, "-test.run=^TestExecRunnerHelperProcess$", "--", "success")
		if err != nil {
			t.Fatalf("execRunner.Run() error = %v", err)
		}
		if got, want := string(output), `{"result":{"agents":[]}}`; got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	})

	t.Run("failure retains stderr diagnostic", func(t *testing.T) {
		_, err := (execRunner{}).Run(context.Background(), executable, "-test.run=^TestExecRunnerHelperProcess$", "--", "failure")
		if err == nil || !strings.Contains(err.Error(), "future warning") {
			t.Fatalf("execRunner.Run() error = %v, want stderr diagnostic", err)
		}
	})
}

func TestExecRunnerHelperProcess(t *testing.T) {
	if len(os.Args) == 0 {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "success":
		fmt.Fprint(os.Stdout, `{"result":{"agents":[]}}`)
		fmt.Fprint(os.Stderr, "future warning")
		os.Exit(0)
	case "failure":
		fmt.Fprint(os.Stderr, "future warning")
		os.Exit(7)
	}
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

	runner := &fakeRunner{responses: [][]byte{[]byte(`{"result":{"agents":[{"agent":"claude","agent_status":"idle","foreground_cwd":"/work","pane_id":"w1:p1","terminal_title":"OC | Session title","tokens":{"ctx":"92k","ctx_raw":"92481"},"agent_session":{"source":"herdr:claude","agent":"claude","kind":"path","value":"/tmp/session.jsonl"}}]}}`)}}
	client := NewClient("/nix/store/herdr", runner)
	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(agents) != 1 || agents[0].PaneID != "w1:p1" {
		t.Fatalf("ListAgents() = %#v", agents)
	}
	// The fixture carries herdr's source and agent fields as well, and they
	// must be dropped rather than fail the decode: only the label and the value
	// are read.
	want := provider.AgentSession{Kind: "path", Value: "/tmp/session.jsonl"}
	if got := agents[0].AgentSession; got == nil || *got != want {
		t.Fatalf("agent session = %#v, want %#v", got, want)
	}
	if agents[0].Tokens["ctx_raw"] != "92481" {
		t.Fatalf("tokens = %#v", agents[0].Tokens)
	}
	if agents[0].TerminalTitle != "OC | Session title" {
		t.Fatalf("terminal title = %q", agents[0].TerminalTitle)
	}
}

func TestListPanes(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{responses: [][]byte{[]byte(`{"result":{"panes":[{"pane_id":"w1:p1","tokens":{"ctx":"92k","ctx_raw":"92481"}},{"pane_id":"w1:p2"}]}}`)}}
	client := NewClient("/nix/store/herdr", runner)
	panes, err := client.ListPanes(context.Background())
	if err != nil {
		t.Fatalf("ListPanes() error = %v", err)
	}
	if len(panes) != 2 || panes[0].PaneID != "w1:p1" || panes[0].Tokens["ctx_raw"] != "92481" {
		t.Fatalf("ListPanes() = %#v", panes)
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

	// The source is spelled out rather than read from SourceID: herdr consumes a
	// source slot permanently and clears tokens only per source, so a renamed
	// source would strand every pane's existing ctx tokens.
	want := []recordedCall{
		{name: "herdr", args: []string{"pane", "report-metadata", "w1:p1", "--source", "herdr-context-usage", "--token", "ctx=92k", "--token", "ctx_raw=92481"}},
		{name: "herdr", args: []string{"pane", "report-metadata", "w1:p1", "--source", "herdr-context-usage", "--clear-token", "ctx", "--clear-token", "ctx_raw"}},
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
		if _, err := client.ListPanes(context.Background()); err == nil {
			t.Fatal("ListPanes() error = nil")
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
		runner := &fakeRunner{responses: [][]byte{[]byte("{"), []byte("{"), []byte("{")}}
		client := NewClient("herdr", runner)
		if _, err := client.ListAgents(context.Background()); err == nil {
			t.Fatal("ListAgents() error = nil")
		}
		if _, err := client.ProcessInfo(context.Background(), "w1:p1"); err == nil {
			t.Fatal("ProcessInfo() error = nil")
		}
		if _, err := client.ListPanes(context.Background()); err == nil {
			t.Fatal("ListPanes() error = nil")
		}
	})

	t.Run("exec runner reports spawn failure", func(t *testing.T) {
		if _, err := (execRunner{}).Run(context.Background(), "herdr-context-usage-command-that-does-not-exist"); err == nil {
			t.Fatal("execRunner.Run() error = nil")
		}
	})
}
