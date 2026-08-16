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

	if !Claude.Implemented() {
		t.Fatal("Claude provider should be implemented")
	}
	if !OpenCode.Implemented() {
		t.Fatal("opencode v1 provider should be implemented")
	}
	for _, kind := range []Kind{Codex, OpenCode2, None} {
		if kind.Implemented() {
			t.Fatalf("%q provider should not be implemented", kind)
		}
	}
}
