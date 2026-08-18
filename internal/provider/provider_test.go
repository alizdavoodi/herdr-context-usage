package provider

import (
	"slices"
	"testing"
)

func TestFromAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		agent     string
		processes ProcessList
		want      Kind
	}{
		{name: "herdr claude wins over a child codex process", agent: "claude", processes: ProcessList{Available: true, Entries: []Process{{Argv0: "codex"}, {Argv0: "claude"}}}, want: Claude},
		{name: "herdr codex is unambiguous", agent: "codex", processes: ProcessList{Available: true, Entries: []Process{{Argv0: "claude"}, {Argv0: "codex"}}}, want: Codex},
		{name: "opencode2 wins regardless of process order", agent: "opencode", processes: ProcessList{Available: true, Entries: []Process{{Argv0: "opencode"}, {Argv0: ".OpenCode2"}}}, want: OpenCode2},
		{name: "opencode v1", agent: "opencode", processes: ProcessList{Available: true, Entries: []Process{{Argv0: "opencode-helper"}, {Argv0: "opencode"}}}, want: OpenCode},
		{name: "opencode v1 with observed empty process list", agent: "opencode", processes: ProcessList{Available: true, Entries: []Process{}}, want: OpenCode},
		{name: "opencode version undeterminable without process list", agent: "opencode", processes: ProcessList{}, want: None},
		// The opencode pair needs the process list to tell its two versions
		// apart, and returns None without one. pi must not inherit that: herdr
		// labels it on its own, so a pane whose processes could not be read is
		// still measurable. This is the only pi case because it is the strongest:
		// the pi arm returns before it reads the list at all, so a case supplying
		// one exercises the same arm with an argument nothing looks at.
		{name: "pi needs no process list", agent: "pi", processes: ProcessList{}, want: Pi},
		{name: "unrecognised agent", agent: "unknown", processes: ProcessList{Available: true, Entries: []Process{{Argv0: "claude"}}}, want: None},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FromAgent(tt.agent, tt.processes); got != tt.want {
				t.Fatalf("FromAgent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessIDsUsesExactBasenames(t *testing.T) {
	t.Parallel()

	processes := []Process{{PID: 1, Argv0: "claude-mcp"}, {PID: 2, Argv0: "/bin/claude"}, {PID: 3, Argv0: "codex"}}
	if got := ProcessIDs(processes, Claude); !slices.Equal(got, []int{2}) {
		t.Fatalf("ProcessIDs() = %v, want [2]", got)
	}
}

func TestImplemented(t *testing.T) {
	t.Parallel()

	// This list is hand-written because Go cannot enumerate a string-typed
	// constant set, and the app package cannot be imported here to derive it
	// from the provider registry without a cycle. internal/app/seam_test.go
	// closes that gap from the other side, asserting every registered provider's
	// kind reports Implemented.
	for _, kind := range []Kind{Claude, Codex, OpenCode, OpenCode2, Pi} {
		if !kind.Implemented() {
			t.Fatalf("%q provider should be implemented", kind)
		}
	}
	for _, kind := range []Kind{None} {
		if kind.Implemented() {
			t.Fatalf("%q provider should not be implemented", kind)
		}
	}
}

func TestNeedsProcessList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		agent string
		want  bool
	}{
		{agent: "opencode", want: true},
		{agent: "claude", want: false},
		{agent: "codex", want: false},
		{agent: "opencode2", want: false},
		{agent: "unknown", want: false},
	}
	for _, tt := range tests {
		if got := NeedsProcessList(tt.agent); got != tt.want {
			t.Errorf("NeedsProcessList(%q) = %v, want %v", tt.agent, got, tt.want)
		}
	}
}
