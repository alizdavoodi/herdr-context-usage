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
		processes []Process
		want      Kind
	}{
		// The child has to be one Process.Kind recognises, or a process that
		// outranked the label would still classify as nothing.
		{name: "herdr claude wins over a child opencode2 process", agent: "claude", processes: []Process{{Argv0: "opencode2"}, {Argv0: "claude"}}, want: Claude},
		{name: "herdr codex is unambiguous", agent: "codex", processes: []Process{{Argv0: "claude"}, {Argv0: "codex"}}, want: Codex},
		{name: "opencode2 wins regardless of process order", agent: "opencode", processes: []Process{{Argv0: "opencode"}, {Argv0: ".OpenCode2"}}, want: OpenCode2},
		{name: "opencode v1", agent: "opencode", processes: []Process{{Argv0: "opencode-helper"}, {Argv0: "opencode"}}, want: OpenCode},
		{name: "opencode v1 with observed empty process list", agent: "opencode", processes: []Process{}, want: OpenCode},
		// herdr 0.9.3 on Linux reports no argv0 for any process: the native
		// process detection writes None where macOS fills the field, while
		// both platforms populate argv. The first argv element is the same
		// argv[0] macOS names, so classification reads it when argv0 is
		// absent — without it, every opencode pane on Linux reads as v1.
		{name: "opencode2 from argv where herdr reports no argv0", agent: "opencode", processes: []Process{{PID: 42, Argv: []string{"/nix/store/x-opencode2-2.0.24/bin/opencode2"}}}, want: OpenCode2},
		{name: "opencode v1 from argv where herdr reports no argv0", agent: "opencode", processes: []Process{{PID: 42, Argv: []string{"/nix/store/x-opencode/bin/opencode"}}}, want: OpenCode},
		// A reported argv0 is herdr's own answer and wins over argv, so the
		// fallback cannot override an explicit name with a different one.
		{name: "reported argv0 wins over argv", agent: "opencode", processes: []Process{{PID: 42, Argv0: "opencode", Argv: []string{"/nix/store/x-opencode2/bin/opencode2"}}}, want: OpenCode},
		// The opencode pair needs the process list to tell its two versions
		// apart, so a pane whose list could not be read is never classified.
		// pi must not inherit that: herdr labels it on its own, so a pane whose
		// processes could not be read is still measurable, which
		// TestNeedsProcessList pins. This is the only pi case because it is the
		// strongest: the pi arm returns before it reads the list at all, so a
		// case supplying one exercises the same arm with an argument nothing
		// looks at.
		{name: "pi needs no process list", agent: "pi", processes: nil, want: Pi},
		// omp is its own label rather than a variant of pi's, so a pane running
		// it must not resolve to the pi kind and read pi's store.
		{name: "omp needs no process list", agent: "omp", processes: nil, want: Omp},
		{name: "hermes needs no process list", agent: "hermes", processes: nil, want: Hermes},
		{name: "unrecognised agent", agent: "unknown", processes: []Process{{Argv0: "claude"}}, want: None},
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

	processes := []Process{
		{PID: 1, Argv0: "claude-mcp"},
		{PID: 2, Argv0: "/bin/claude"},
		{PID: 3, Argv0: "codex"},
		// The argv fallback of Process.Kind, so claude's registry tier keeps
		// resolving process ids on a herdr that reports no argv0.
		{PID: 4, Argv: []string{"/nix/store/x-claude/bin/claude"}},
		{PID: 5, Argv0: "claude", Argv: []string{"/bin/not-claude"}},
	}
	if got := ProcessIDs(processes, Claude); !slices.Equal(got, []int{2, 4, 5}) {
		t.Fatalf("ProcessIDs() = %v, want [2 4 5]", got)
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
		{agent: "pi", want: false},
		{agent: "omp", want: false},
		{agent: "hermes", want: false},
		{agent: "unknown", want: false},
	}
	for _, tt := range tests {
		if got := NeedsProcessList(tt.agent); got != tt.want {
			t.Errorf("NeedsProcessList(%q) = %v, want %v", tt.agent, got, tt.want)
		}
	}
}
