package pi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// The two session directory names are the ones pi actually created on this
// machine, measured against pi 0.84.2: it wraps the whole absolute path in a
// leading and trailing "--" and preserves dots, so "github.com" is not slugged.
// Nothing here derives such a name — resolution never constructs one — but a
// fixture layout that used a plausible-looking slug instead would hide that.
const (
	repoSessionDir = "--Users-alirezadavoodi-projects-github.com-alizdavoodi-herdr-context-usage--"
	homeSessionDir = "--Users-alirezadavoodi--"
)

// The real transcript names from both directories: a start timestamp, an
// underscore, the session id, ".jsonl".
const (
	repoTranscript = "2026-08-18T12-34-48-971Z_01a014de-020a-70b2-bc8e-b6a70ed3e784.jsonl"
	repoSessionID  = "01a014de-020a-70b2-bc8e-b6a70ed3e784"
	homeTranscript = "2026-08-16T20-45-15-107Z_01a00c52-4be3-7232-a529-2c4322257dd6.jsonl"
	homeSessionID  = "01a00c52-4be3-7232-a529-2c4322257dd6"
)

func TestResolveUsesAReportedPathDirectly(t *testing.T) {
	t.Parallel()

	// The path is used as reported and is not required to exist: a resolved
	// transcript that is not there is the reader's evidence that the agent
	// session is gone, and confusing the two would blank the row for the wrong
	// reason.
	//
	// What a label and a value mean is provider.AgentSession's one rule, tested
	// there; this case pins only that a path it reports reaches the reader.
	reported := filepath.Join(t.TempDir(), repoSessionDir, repoTranscript)
	got, err := resolve(t.TempDir(), &provider.AgentSession{Kind: "path", Value: reported})
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if got != reported {
		t.Fatalf("resolve() = %q, want %q", got, reported)
	}
}

// A pane reporting an absolute path is fully resolvable without a home, so a
// home that cannot be resolved at all must not sink it. Unsetting HOME with no
// configured home is the only way to make the home lookup fail, so this case
// sets the environment and cannot run in parallel.
func TestResolveAPathWithoutAResolvableHome(t *testing.T) {
	t.Setenv("HOME", "")

	want := filepath.Join(t.TempDir(), "sessions", repoSessionDir, repoTranscript)
	got, err := resolve("", &provider.AgentSession{Kind: "path", Value: want})
	if err != nil {
		t.Fatalf("resolve() error = %v, want the reported path used without a home", err)
	}
	if got != want {
		t.Fatalf("resolve() = %q, want %q", got, want)
	}
}

func TestResolveMatchesAReportedIDToItsTranscript(t *testing.T) {
	t.Parallel()

	// Two transcripts in two session directories, as the real store has, so the
	// match has to pick the one whose name ends with the reported id rather than
	// the only file present.
	home := t.TempDir()
	want := writeStore(t, home, map[string][]string{
		repoSessionDir: {repoTranscript},
		homeSessionDir: {homeTranscript},
	})[repoTranscript]

	got, err := resolve(home, &provider.AgentSession{Kind: "id", Value: repoSessionID})
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if got != want {
		t.Fatalf("resolve() = %q, want %q", got, want)
	}
}

func TestResolveTreatsANonUniqueIDAsUnresolved(t *testing.T) {
	t.Parallel()

	// The same id filed under two session directories, which is what a resumed
	// agent session started from a second working directory would leave behind.
	// Their names carry different start timestamps and their mtimes differ, so a
	// reader that broke the tie by recency, by name or by start time would
	// resolve to one of them: ADR-0004 measured recency picking the wrong
	// session, so a non-unique match is unresolved and the row goes blank rather
	// than being guessed at.
	home := t.TempDir()
	writeStore(t, home, map[string][]string{
		repoSessionDir: {repoTranscript},
		homeSessionDir: {"2026-08-19T09-00-00-000Z_" + repoSessionID + ".jsonl"},
	})

	if _, err := resolve(home, &provider.AgentSession{Kind: "id", Value: repoSessionID}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("resolve() error = %v, want %v", err, ErrNoSession)
	}
}

func TestResolveRejectsUnmatchableIDs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeStore(t, home, map[string][]string{repoSessionDir: {repoTranscript}})

	tests := []struct {
		name  string
		value string
	}{
		{name: "no transcript carries the id", value: homeSessionID},
		{
			// The id has to be the end of the file name. A prefix of a real id
			// matches nothing, which is why the pattern is anchored to ".jsonl"
			// rather than being a substring search.
			name:  "a partial id is not a match",
			value: "01a014de",
		},
		{
			// A directory name is not a session id either, and the pattern's own
			// wildcard already covers exactly one directory below sessions/.
			name:  "a session directory name is not a session id",
			value: repoSessionDir,
		},
		// A value carrying a glob metacharacter is not a session id — a real one
		// is a UUID — so it is refused before it can widen the pattern. The
		// separator and escape refusals are pinned beside provider.SessionTranscript
		// by ids that would resolve without them.
		{name: "star", value: "*"},
		{name: "star inside an id", value: "01a014de-020a-70b2-bc8e-b6a70ed3*"},
		{name: "question mark", value: "01a014de-020a-70b2-bc8e-b6a70ed3e78?"},
		{name: "character class", value: "01a014de-020a-70b2-bc8e-b6a70ed3e78[0-9]"},
		// Labelled an id, even the absolute path of a real transcript is not
		// read as one: the agent said it was an id.
		{name: "an absolute path", value: filepath.Join(home, "sessions", repoSessionDir, repoTranscript)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := resolve(home, &provider.AgentSession{Kind: "id", Value: tt.value}); !errors.Is(err, ErrNoSession) {
				t.Fatalf("resolve(%q) error = %v, want %v", tt.value, err, ErrNoSession)
			}
		})
	}
}

func TestResolveWithoutAReportedAgentSession(t *testing.T) {
	t.Parallel()

	// herdr's reported agent session is the whole of the input, so nothing
	// reported means nothing to measure. There is no working-directory fallback
	// to reach for: pi files an agent session under the directory it started in,
	// the transcript does not mark a sub-agent session, and recency is banned.
	home := t.TempDir()
	writeStore(t, home, map[string][]string{repoSessionDir: {repoTranscript}})

	tests := []struct {
		name    string
		session *provider.AgentSession
	}{
		{name: "no agent session at all", session: nil},
		{name: "an agent session with no value", session: &provider.AgentSession{Kind: "id"}},
		{name: "a path kind with no value", session: &provider.AgentSession{Kind: "path"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := resolve(home, tt.session); !errors.Is(err, ErrNoSession) {
				t.Fatalf("resolve(%#v) error = %v, want %v", tt.session, err, ErrNoSession)
			}
		})
	}
}

// With no configured home, pi's store is looked for under ~/.pi/agent. Which
// home wins when one is configured is pinned once, in the provider root; what
// stays here is this provider's own default. Sets HOME, so it cannot run in
// parallel.
func TestResolveDefaultsToPisOwnHome(t *testing.T) {
	fromDefault := t.TempDir()
	want := writeStore(t, filepath.Join(fromDefault, ".pi", "agent"), map[string][]string{repoSessionDir: {repoTranscript}})[repoTranscript]
	t.Setenv("HOME", fromDefault)

	got, err := resolve("", &provider.AgentSession{Kind: "id", Value: repoSessionID})
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if got != want {
		t.Fatalf("resolve() = %q, want the default ~/.pi/agent home %q", got, want)
	}
}

// writeStore lays out empty transcripts under home/sessions and returns each
// file's path keyed by its name. The files are empty because resolution reads
// names only; a transcript with contents would suggest otherwise.
func writeStore(t *testing.T, home string, layout map[string][]string) map[string]string {
	t.Helper()

	paths := make(map[string]string, len(layout))
	for dir, names := range layout {
		sessionDir := filepath.Join(home, "sessions", dir)
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			path := filepath.Join(sessionDir, name)
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			paths[name] = path
		}
	}
	return paths
}
