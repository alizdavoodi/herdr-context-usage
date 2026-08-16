package provider

import "testing"

func TestFromProcesses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		processes []Process
		want      Kind
	}{
		{name: "claude", processes: []Process{{Argv0: "claude"}}, want: Claude},
		{name: "codex is routed for its later provider", processes: []Process{{Argv0: "/nix/store/hash/bin/codex"}}, want: Codex},
		{name: "opencode2 wins over its opencode prefix", processes: []Process{{Argv0: "opencode2"}}, want: OpenCode2},
		{name: "opencode v1", processes: []Process{{Argv0: "opencode"}}, want: OpenCode},
		{name: "nested helper does not hide agent", processes: []Process{{Argv0: "node"}, {Argv0: "claude"}}, want: Claude},
		{name: "unrecognised", processes: []Process{{Argv0: "zsh"}}, want: None},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FromProcesses(tt.processes); got != tt.want {
				t.Fatalf("FromProcesses() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestImplemented(t *testing.T) {
	t.Parallel()

	if !Claude.Implemented() {
		t.Fatal("Claude provider should be implemented")
	}
	for _, kind := range []Kind{Codex, OpenCode, OpenCode2, None} {
		if kind.Implemented() {
			t.Fatalf("%q provider should not be implemented in the walking skeleton", kind)
		}
	}
}
