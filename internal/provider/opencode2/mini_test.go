package opencode2

import "testing"

// The measured invocation is the first case and the only one taken from a live
// pane: opencode2 0.0.0-next-17444 started as `opencode2 mini`, reporting argv0
// "opencode2" and argv [".../bin/opencode2", "mini"], while the pane's terminal
// title read the raw "opencode2 mini" the shell had written and its row stayed
// blank. Everything below it is constructed to pin the rule's edges, and the
// edges matter more than the centre here: a missed detection leaves the row
// exactly as it is today, while a false one attaches a confidently wrong reason
// to it.
func TestRunsMiniInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		want bool
	}{
		{
			name: "the measured invocation",
			argv: []string{"/nix/store/x-opencode2-0.0.0-next-17444/bin/opencode2", "mini"},
			want: true,
		},
		{
			// Whether opencode2's global flags may precede a subcommand was never
			// established, so the rule skips leading flags rather than indexing a
			// fixed position.
			name: "a global flag precedes the subcommand",
			argv: []string{"opencode2", "--wizard", "mini"},
			want: true,
		},
		{
			// The false positive worth refusing: mini is this prompt's text, not
			// the subcommand. Scanning the whole of argv for the word would claim
			// a mini interface for a pane running an ordinary TUI.
			name: "mini appears as an argument rather than the subcommand",
			argv: []string{"opencode2", "run", "mini"},
			want: false,
		},
		{name: "the ordinary TUI takes no subcommand", argv: []string{"opencode2"}, want: false},
		{name: "another subcommand", argv: []string{"opencode2", "serve"}, want: false},
		{name: "no argv reported at all", argv: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := runsMiniInterface(tt.argv); got != tt.want {
				t.Fatalf("runsMiniInterface(%q) = %t, want %t", tt.argv, got, tt.want)
			}
		})
	}
}
