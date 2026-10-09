package provider

import (
	"errors"
	"testing"
)

var errRead = errors.New("process info failed")

func TestFromAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		agent      string
		processes  []Process
		processErr error
		want       Kind
		wantOK     bool
	}{
		// The child has to be one Process.Kind recognises, or a process that
		// outranked the label would still classify as nothing.
		{name: "herdr claude wins over a child opencode2 process", agent: "claude", processes: []Process{{Argv0: "opencode2"}, {Argv0: "claude"}}, want: Claude, wantOK: true},
		{name: "herdr codex is unambiguous", agent: "codex", processes: []Process{{Argv0: "claude"}, {Argv0: "codex"}}, want: Codex, wantOK: true},
		{name: "opencode2 wins regardless of process order", agent: "opencode", processes: []Process{{Argv0: "opencode"}, {Argv0: ".OpenCode2"}}, want: OpenCode2, wantOK: true},
		{name: "opencode v1", agent: "opencode", processes: []Process{{Argv0: "opencode-helper"}, {Argv0: "opencode"}}, want: OpenCode, wantOK: true},
		{name: "opencode v1 with observed empty process list", agent: "opencode", processes: []Process{}, want: OpenCode, wantOK: true},
		// A failed read is evidence of nothing, so the pane is not classified at
		// all rather than defaulting to v1.
		{name: "opencode with a failed read is not classified", agent: "opencode", processErr: errRead, want: None, wantOK: false},
		// Only opencode's classification depends on the list; a label that
		// names its kind outright is classified whatever the read did.
		{name: "claude with a failed read", agent: "claude", processErr: errRead, want: Claude, wantOK: true},
		{name: "opencode2 with a failed read", agent: "opencode2", processErr: errRead, want: OpenCode2, wantOK: true},
		// The opencode pair needs the process list to tell its two versions
		// apart, so a pane whose list could not be read is never classified.
		// pi must not inherit that: herdr labels it on its own, so a pane whose
		// processes could not be read is still measurable, which
		// TestReadsProcesses pins. This is the only pi case because it is the
		// strongest: the pi arm returns before it reads the list at all, so a
		// case supplying one exercises the same arm with an argument nothing
		// looks at.
		{name: "pi needs no process list", agent: "pi", processes: nil, want: Pi, wantOK: true},
		// omp is its own label rather than a variant of pi's, so a pane running
		// it must not resolve to the pi kind and read pi's store.
		{name: "omp needs no process list", agent: "omp", processes: nil, want: Omp, wantOK: true},
		{name: "hermes needs no process list", agent: "hermes", processes: nil, want: Hermes, wantOK: true},
		{name: "unrecognised agent", agent: "unknown", processes: []Process{{Argv0: "claude"}}, want: None, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := FromAgent(tt.agent, tt.processes, tt.processErr); got != tt.want || ok != tt.wantOK {
				t.Fatalf("FromAgent() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// pi, omp and hermes are labelled by herdr on their own and none of their
// providers reads the list, so their panes never pay for the fetch.
func TestReadsProcesses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		agent string
		want  bool
	}{
		{agent: "opencode", want: true},
		{agent: " Claude ", want: true},
		{agent: "codex", want: false},
		{agent: "opencode2", want: true},
		{agent: "pi", want: false},
		{agent: "omp", want: false},
		{agent: "hermes", want: false},
		{agent: "unknown", want: false},
	}
	for _, tt := range tests {
		if got := ReadsProcesses(tt.agent); got != tt.want {
			t.Errorf("ReadsProcesses(%q) = %v, want %v", tt.agent, got, tt.want)
		}
	}
}
