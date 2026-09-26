package opencodeshared

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The longest root-session title in the live opencode2 store, at 52 characters:
// session ses_ff99868e7ffeiAS9uOUUAFWrpU. The TUI writes it as the first 37
// characters plus an ellipsis, which is the only form this plugin ever sees.
const realLongTitle = "Fix Codex review findings with failing/passing tests"

// realTruncatedTitle is what the TUI writes for it: slice(0,37) plus "...".
var realTruncatedTitle = realLongTitle[:37]

func TestParseTitleRecognisesEveryForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		terminalTitle string
		want          Title
		wantErr       error
	}{
		{
			// The title of the live opencode2 pane this plugin was verified
			// against, reported by herdr as `OC | Probe test response`.
			name:          "session title",
			terminalTitle: "OC | Probe test response",
			want:          Title{Text: "Probe test response"},
		},
		{
			name:          "truncated session title",
			terminalTitle: "OC | " + realTruncatedTitle + "...",
			want:          Title{Text: realTruncatedTitle, Truncated: true},
		},
		{
			// Both TUIs write the bare application name on their home screen and
			// for a session with no title; roughly 10% of sessions are untitled.
			name:          "home screen or untitled session",
			terminalTitle: "OpenCode",
			wantErr:       ErrUntitled,
		},
		{
			// A plugin route interpolates a plugin id (v1) or name (v2) into the
			// same shape. Neither is ever shortened, so one longer than a session
			// title can be is the one form we can tell apart with certainty.
			name:          "plugin route title too long to be a session title",
			terminalTitle: "OC | " + strings.Repeat("plugin-", 8),
			wantErr:       ErrPluginTitle,
		},
		{
			// The boundary, written out rather than derived from the constant
			// under test, because a test that reads maxSessionTitle moves with any
			// change to it and stops being a boundary at all. The TUI shortens a
			// title only when it is longer than 40, so 40 arrives whole and is a
			// session title; 29 root sessions across the two live stores are
			// exactly this length, and reading them as plugin routes would blank
			// every one of them.
			name:          "session title of exactly forty characters is not a plugin",
			terminalTitle: "OC | " + strings.Repeat("x", 40),
			want:          Title{Text: strings.Repeat("x", 40)},
		},
		{
			// One character past it, with no ellipsis, cannot have come from the
			// TUI's session path at all.
			name:          "unshortened title of forty-one characters is a plugin",
			terminalTitle: "OC | " + strings.Repeat("x", 41),
			wantErr:       ErrPluginTitle,
		},
		{
			// A title counted in bytes rather than runes would be dismissed as a
			// plugin's and the pane would blank with the wrong explanation. 40
			// two-byte runes are 80 bytes, twice the limit.
			name:          "long multi-byte session title stays a session title",
			terminalTitle: "OC | " + strings.Repeat("é", 40),
			want:          Title{Text: strings.Repeat("é", 40)},
		},
		{name: "another program's title", terminalTitle: "zsh", wantErr: ErrTitleUnavailable},
		{name: "no title at all", terminalTitle: "", wantErr: ErrTitleUnavailable},
		{name: "prefix with nothing after it", terminalTitle: "OC | ", wantErr: ErrTitleUnavailable},
		{name: "nothing but a truncation mark", terminalTitle: "OC | ...", wantErr: ErrTitleUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTitle(tt.terminalTitle)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("parseTitle() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("parseTitle() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseTitleExplainsTheDisabledTerminalTitle(t *testing.T) {
	// Not parallel: the environment is process-wide.
	t.Setenv("OPENCODE_DISABLE_TERMINAL_TITLE", "true")

	_, err := parseTitle("zsh")
	if !errors.Is(err, ErrTitleDisabled) {
		t.Fatalf("parseTitle() error = %v, want the disabled-title explanation", err)
	}
	if !errors.Is(err, ErrTitleUnavailable) {
		t.Fatalf("parseTitle() error = %v, want an unavailable title as well, so one declaration covers both", err)
	}
	if !strings.Contains(err.Error(), "OPENCODE_DISABLE_TERMINAL_TITLE") {
		t.Fatalf("diagnostic = %q, want it to name the variable", err.Error())
	}
	// A pane that still reports a title is unaffected: the variable is read from
	// our environment, which is only a hint about the agent's.
	if _, err := parseTitle("OC | Probe test response"); err != nil {
		t.Fatalf("parseTitle() error = %v, want a usable title to survive the hint", err)
	}
}

func TestTitleDisabledFollowsOpenCodesOwnTruthiness(t *testing.T) {
	// opencode 1.18.18 lowercases the value and accepts only "true" and "1".
	for value, want := range map[string]bool{
		"true": true, "TRUE": true, "1": true,
		"": false, "0": false, "false": false, "yes": false,
	} {
		t.Setenv("OPENCODE_DISABLE_TERMINAL_TITLE", value)
		if got := titleDisabled(); got != want {
			t.Fatalf("titleDisabled() with %q = %v, want %v", value, got, want)
		}
	}
}

func TestComparisonMatchesWholeTitlesExactlyAndTruncatedOnesByPrefix(t *testing.T) {
	t.Parallel()

	exact, args := Title{Text: "Probe test response"}.Comparison()
	if exact != "title = ?" || len(args) != 1 {
		t.Fatalf("Comparison() = %q, %v; want an exact match", exact, args)
	}
	prefix, args := Title{Text: realTruncatedTitle, Truncated: true}.Comparison()
	if prefix != "substr(title, 1, length(?)) = ?" || len(args) != 2 {
		t.Fatalf("Comparison() = %q, %v; want a prefix match", prefix, args)
	}
}

func TestResolveTitleRefusesToGuess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		directory string
		matches   []string
		want      string
		wantErr   error
	}{
		{name: "one match resolves", directory: "/work", matches: []string{"ses_one"}, want: "ses_one"},
		{name: "two matches stay blank", directory: "/work", matches: []string{"ses_one", "ses_two"}, wantErr: ErrNoTitleMatch},
		{name: "no match stays blank", directory: "/work", matches: nil, wantErr: ErrNoTitleMatch},
		{
			// Titles are not unique across directories: 14 sessions share one
			// repository in the sampled store.
			name:      "no directory to disambiguate with",
			directory: "",
			matches:   []string{"ses_one"},
			wantErr:   ErrNoTitleMatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lookup := func(context.Context, string, Title) ([]string, error) { return tt.matches, nil }
			got, err := ResolveTitle(context.Background(), tt.directory, "OC | Probe test response", lookup)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveTitle() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ResolveTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTitleReportsLookupFailureAsItself(t *testing.T) {
	t.Parallel()

	// A failing query is a transient database failure, not evidence about the
	// session, so it must not be flattened into a blank-the-pane answer.
	want := errors.New("database is locked")
	lookup := func(context.Context, string, Title) ([]string, error) { return nil, want }
	if _, err := ResolveTitle(context.Background(), "/work", "OC | Probe test response", lookup); !errors.Is(err, want) {
		t.Fatalf("ResolveTitle() error = %v, want %v", err, want)
	}
}

func TestResolveTitlePassesTheParsedTitleToTheStore(t *testing.T) {
	t.Parallel()

	var got Title
	var gotDirectory string
	lookup := func(_ context.Context, directory string, title Title) ([]string, error) {
		gotDirectory, got = directory, title
		return []string{"ses_one"}, nil
	}
	if _, err := ResolveTitle(context.Background(), "/work/acme", "OC | "+realTruncatedTitle+"...", lookup); err != nil {
		t.Fatalf("ResolveTitle() error = %v", err)
	}
	if gotDirectory != "/work/acme" || got != (Title{Text: realTruncatedTitle, Truncated: true}) {
		t.Fatalf("lookup received %q, %#v; want the pane directory and the parsed title", gotDirectory, got)
	}
}
