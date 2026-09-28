package opencodeshared

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// The longest root-session title in the live opencode2 store, at 52 characters:
// session ses_ff99868e7ffeiAS9uOUUAFWrpU. The TUI writes it as the first 37
// characters plus an ellipsis, which is the only form this plugin ever sees.
const realLongTitle = "Fix Codex review findings with failing/passing tests"

// realTruncatedTitle is what the TUI writes for it, before the truncation mark:
// slice(0,37).
var realTruncatedTitle = realLongTitle[:37]

func TestParseTitleRecognisesEveryForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		terminalTitle string
		want          parsedTitle
		wantErr       error
	}{
		{
			// The title of the live opencode2 pane this plugin was verified
			// against, reported by herdr as `OC | Probe test response`.
			name:          "session title",
			terminalTitle: "OC | Probe test response",
			want:          parsedTitle{Text: "Probe test response"},
		},
		{
			name:          "truncated session title",
			terminalTitle: "OC | " + realTruncatedTitle + "...",
			want:          parsedTitle{Text: realTruncatedTitle, Truncated: true},
		},
		{
			// opencode 1.18.32 and opencode2 2.0.18 shorten with one U+2026
			// where earlier builds wrote three dots. Read as a whole title, it matches no session at all:
			// 23 of the live store's 110 root sessions are long enough.
			name:          "truncated session title with a unicode ellipsis",
			terminalTitle: "OC | " + realTruncatedTitle + "…",
			want:          parsedTitle{Text: realTruncatedTitle, Truncated: true},
		},
		{
			// The TUI marks only what it cut at 37 UTF-16 units, so a short
			// title that ends in an ellipsis is the whole title. Read as a
			// prefix, it would be ambiguous beside any longer title it begins.
			name:          "short session title ending in an ellipsis is whole",
			terminalTitle: "OC | Investigate…",
			want:          parsedTitle{Text: "Investigate…"},
		},
		{
			// A plugin name is never shortened, so read as truncated this one
			// would slip past the plugin guard and prefix-match a session.
			name:          "long plugin title ending in an ellipsis is a plugin",
			terminalTitle: "OC | Plugin dashboard showing context occupancy details…",
			wantErr:       ErrPluginTitle,
		},
		{
			// An astral character is two UTF-16 units, so the TUI's 37 units
			// here are 36 runes; counting runes would read this as whole.
			name:          "truncated title with an astral character",
			terminalTitle: "OC | 🚀" + realTruncatedTitle[:35] + "…",
			want:          parsedTitle{Text: "🚀" + realTruncatedTitle[:35], Truncated: true},
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
			want:          parsedTitle{Text: strings.Repeat("x", 40)},
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
			want:          parsedTitle{Text: strings.Repeat("é", 40)},
		},
		{name: "another program's title", terminalTitle: "zsh", wantErr: ErrTitleUnavailable},
		{name: "no title at all", terminalTitle: "", wantErr: ErrTitleUnavailable},
		{name: "prefix with nothing after it", terminalTitle: "OC | ", wantErr: ErrTitleUnavailable},
		{name: "nothing but a truncation mark", terminalTitle: "OC | ...", wantErr: ErrTitleUnavailable},
		// An empty prefix would match every root session in the directory, and
		// the sole one in it would be reported as this pane's.
		{name: "nothing but a unicode truncation mark", terminalTitle: "OC | …", wantErr: ErrTitleUnavailable},
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

// session is one row of the minimal sessions table below: the four columns the
// title query reads, with an empty parent for a root session.
type session struct{ id, directory, title, parent string }

// sessionsDB is a store holding only sessions, in a table this test names for
// itself. Each provider pins the query against its own real table.
func sessionsDB(t *testing.T, sessions ...session) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, directory TEXT, title TEXT, parent_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		var parent any
		if s.parent != "" {
			parent = s.parent
		}
		if _, err := db.Exec(`INSERT INTO sessions VALUES (?, ?, ?, ?)`, s.id, s.directory, s.title, parent); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestResolveTitleRefusesToGuess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sessions      []session
		directory     string
		terminalTitle string
		want          string
		wantErr       error
	}{
		{
			name:          "a whole title matches its root session",
			sessions:      []session{{id: "ses_one", directory: "/work", title: "Probe test response"}},
			directory:     "/work",
			terminalTitle: "OC | Probe test response",
			want:          "ses_one",
		},
		{
			name:          "a truncated title matches the root session it is a prefix of",
			sessions:      []session{{id: "ses_long", directory: "/work", title: realLongTitle}},
			directory:     "/work",
			terminalTitle: "OC | " + realTruncatedTitle + "...",
			want:          "ses_long",
		},
		{
			// Only a truncated title is compared as a prefix; a whole one must be
			// the stored title exactly.
			name:          "a whole title is not a prefix match",
			sessions:      []session{{id: "ses_longer", directory: "/work", title: "Probe test response, continued"}},
			directory:     "/work",
			terminalTitle: "OC | Probe test response",
			wantErr:       ErrNoTitleMatch,
		},
		{
			// The unrelated session is the one a recency fallback would pick.
			name:          "no match stays blank",
			sessions:      []session{{id: "ses_other", directory: "/work", title: "Newer unrelated session"}},
			directory:     "/work",
			terminalTitle: "OC | Probe test response",
			wantErr:       ErrNoTitleMatch,
		},
		{
			name: "two matches stay blank",
			sessions: []session{
				{id: "ses_one", directory: "/work", title: realTruncatedTitle + " first"},
				{id: "ses_two", directory: "/work", title: realTruncatedTitle + " second"},
			},
			directory:     "/work",
			terminalTitle: "OC | " + realTruncatedTitle + "...",
			wantErr:       ErrNoTitleMatch,
		},
		{
			// A child session is a sub-agent's, never the pane's main thread, so
			// it is not a candidate even under the same title.
			name: "a child session is not a candidate",
			sessions: []session{
				{id: "ses_root", directory: "/work", title: "Probe test response"},
				{id: "ses_child", directory: "/work", title: "Probe test response", parent: "ses_root"},
			},
			directory:     "/work",
			terminalTitle: "OC | Probe test response",
			want:          "ses_root",
		},
		{
			name: "another directory's session is not a candidate",
			sessions: []session{
				{id: "ses_here", directory: "/work/acme", title: "Probe test response"},
				{id: "ses_elsewhere", directory: "/work/other", title: "Probe test response"},
			},
			directory:     "/work/acme",
			terminalTitle: "OC | Probe test response",
			want:          "ses_here",
		},
		{
			// Titles are not unique across directories: 14 sessions share one
			// repository in the sampled store.
			name:          "no directory to disambiguate with",
			sessions:      []session{{id: "ses_one", directory: "/work", title: "Probe test response"}},
			directory:     "",
			terminalTitle: "OC | Probe test response",
			wantErr:       ErrNoTitleMatch,
		},
		{
			// An unusable title is answered before the store is asked anything, so
			// no fallback can reach the session sitting there.
			name:          "an unusable title never reaches the store",
			sessions:      []session{{id: "ses_one", directory: "/work", title: "Probe test response"}},
			directory:     "/work",
			terminalTitle: "zsh",
			wantErr:       ErrTitleUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveTitle(context.Background(), sessionsDB(t, tt.sessions...), "sessions", tt.directory, tt.terminalTitle)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveTitle() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ResolveTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A failing query is a failure to look, not evidence about the session, so it
// must surface as itself rather than be flattened into a blank-the-pane answer.
func TestResolveTitleReportsAQueryFailureAsItself(t *testing.T) {
	t.Parallel()

	db := sessionsDB(t, session{id: "ses_one", directory: "/work", title: "Probe test response"})
	_, err := ResolveTitle(context.Background(), db, "no_such_table", "/work", "OC | Probe test response")
	if err == nil || errors.Is(err, ErrNoTitleMatch) || !strings.Contains(err.Error(), "resolve opencode session") {
		t.Fatalf("ResolveTitle() error = %v, want the query failure itself", err)
	}
}
