package omp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// repoSessionDir is the one session directory omp actually created on this
// machine, measured against omp 17.3.5: it strips the home prefix and preserves
// dots, so "github.com" is not slugged and no leading path segment survives.
// This is the divergence from pi, which wraps the whole absolute path in a
// leading and trailing "--" — and the reason each provider owns its own
// derivation even though neither one ever constructs such a name.
//
// otherSessionDir is constructed rather than real: only one slug directory
// exists in the store. It follows the measured derivation for a session started
// one level deeper, which is what a second directory would look like, and it
// exists because a store with one directory cannot show that the id match is
// searching across them rather than taking the only file present.
const (
	repoSessionDir  = "-projects-github.com-alizdavoodi-herdr-context-usage"
	otherSessionDir = "-projects-github.com-alizdavoodi-herdr-context-usage-docs"
)

// The real transcript names from that directory: a start timestamp, an
// underscore, the session id, ".jsonl".
const (
	repoTranscript  = "2026-08-18T10-57-53-412Z_01a01485-4504-7000-ae6b-ace797eca8f5.jsonl"
	repoSessionID   = "01a01485-4504-7000-ae6b-ace797eca8f5"
	otherTranscript = "2026-08-18T12-21-05-301Z_01a014d1-7095-7000-bca9-89174e6f99cd.jsonl"
	otherSessionID  = "01a014d1-7095-7000-bca9-89174e6f99cd"
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
	got, err := Resolve(SessionInput{
		OmpHome:      t.TempDir(),
		AgentSession: &provider.AgentSession{Kind: "path", Value: reported},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != reported || got.Mode != ResolutionAgentSession {
		t.Fatalf("Resolve() = %#v, want %q and agent_session mode", got, reported)
	}
}

// A pane reporting an absolute path is fully resolvable without a home, so a
// home that cannot be resolved at all must not sink it. Unsetting HOME with no
// configured home is the only way to make the home lookup fail, so this case
// sets the environment and cannot run in parallel.
func TestResolveAPathWithoutAResolvableHome(t *testing.T) {
	t.Setenv("HOME", "")

	want := filepath.Join(t.TempDir(), "sessions", repoSessionDir, repoTranscript)
	got, err := Resolve(SessionInput{AgentSession: &provider.AgentSession{Kind: "path", Value: want}})
	if err != nil {
		t.Fatalf("Resolve() error = %v, want the reported path used without a home", err)
	}
	if got.Path != want || got.Mode != ResolutionAgentSession {
		t.Fatalf("Resolve() = %#v, want %q and agent_session mode", got, want)
	}
}

func TestResolveMatchesAReportedIDToItsTranscript(t *testing.T) {
	t.Parallel()

	// Two transcripts in two session directories, so the match has to pick the
	// one whose name ends with the reported id rather than the only file present.
	home := t.TempDir()
	want := writeStore(t, home, map[string][]string{
		repoSessionDir:  {repoTranscript},
		otherSessionDir: {otherTranscript},
	})[repoTranscript]

	got, err := Resolve(SessionInput{
		OmpHome:      home,
		AgentSession: &provider.AgentSession{Kind: "id", Value: repoSessionID},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != want || got.Mode != ResolutionAgentSession {
		t.Fatalf("Resolve() = %#v, want %q and agent_session mode", got, want)
	}
}

// omp files a directory of per-tool log files beside each transcript, named for
// the same start timestamp and session id — this is the real store's shape and
// pi's has nothing like it. The id therefore has a second candidate sitting next
// to the transcript for every agent session, and the ".jsonl" suffix is the only
// thing keeping it out of the match.
func TestResolveIgnoresTheLogDirectoryBesideTheTranscript(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	want := writeStore(t, home, map[string][]string{repoSessionDir: {repoTranscript}})[repoTranscript]
	// The directory name and the first two file names are real, from
	// .../-projects-github.com-alizdavoodi-herdr-context-usage/2026-08-18T10-57-53-412Z_01a01485-.../
	// The third is real too but comes from the 2026-08-18T09-58-11-656Z
	// directory; it is here because that directory holds read logs where this
	// one holds only bash logs, and the suffix must exclude both kinds.
	logs := filepath.Join(home, "sessions", repoSessionDir, "2026-08-18T10-57-53-412Z_"+repoSessionID)
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"14.bash.log", "47.bash-original.log", "1.read.log"} {
		if err := os.WriteFile(filepath.Join(logs, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Resolve(SessionInput{
		OmpHome:      home,
		AgentSession: &provider.AgentSession{Kind: "id", Value: repoSessionID},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v, want the log directory ignored rather than treated as a second match", err)
	}
	if got.Path != want {
		t.Fatalf("Resolve() = %q, want the transcript %q", got.Path, want)
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
		repoSessionDir:  {repoTranscript},
		otherSessionDir: {"2026-08-19T09-00-00-000Z_" + repoSessionID + ".jsonl"},
	})

	if _, err := Resolve(SessionInput{
		OmpHome:      home,
		AgentSession: &provider.AgentSession{Kind: "id", Value: repoSessionID},
	}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrNoSession)
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
		{name: "no transcript carries the id", value: otherSessionID},
		{
			// The id has to be the end of the file name. A prefix of a real id
			// matches nothing, which is why the pattern is anchored to ".jsonl"
			// rather than being a substring search.
			name:  "a partial id is not a match",
			value: "01a01485",
		},
		{
			// A directory name is not a session id either, and the pattern's own
			// wildcard already covers exactly one directory below sessions/.
			name:  "a session directory name is not a session id",
			value: repoSessionDir,
		},
		// A value carrying a glob metacharacter or a path separator is not a
		// session id — a real one is a UUID — so it is refused before it can widen
		// the pattern or escape the store.
		{name: "star", value: "*"},
		{name: "star inside an id", value: "01a01485-4504-7000-ae6b-ace797ec*"},
		{name: "question mark", value: "01a01485-4504-7000-ae6b-ace797eca8f?"},
		{name: "character class", value: "01a01485-4504-7000-ae6b-ace797eca8f[0-9]"},
		{name: "escape", value: `01a01485\4504`},
		{name: "path separator", value: "../" + repoSessionDir + "/" + repoTranscript},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Resolve(SessionInput{
				OmpHome:      home,
				AgentSession: &provider.AgentSession{Kind: "id", Value: tt.value},
			}); !errors.Is(err, ErrNoSession) {
				t.Fatalf("Resolve(%q) error = %v, want %v", tt.value, err, ErrNoSession)
			}
		})
	}
}

func TestResolveWithoutAReportedAgentSession(t *testing.T) {
	t.Parallel()

	// herdr's reported agent session is the whole of the input, so nothing
	// reported means nothing to measure. There is no working-directory fallback
	// to reach for: omp files an agent session under a directory derived from the
	// one it started in, the transcript does not mark a sub-agent session,
	// recency is banned, and an omp pane is not even labelled an agent by herdr
	// until omp's integration is installed.
	home := t.TempDir()
	writeStore(t, home, map[string][]string{repoSessionDir: {repoTranscript}})

	tests := []struct {
		name  string
		input SessionInput
	}{
		{name: "no agent session at all", input: SessionInput{OmpHome: home}},
		{name: "an agent session with no value", input: SessionInput{OmpHome: home, AgentSession: &provider.AgentSession{Kind: "id"}}},
		{name: "a path kind with no value", input: SessionInput{OmpHome: home, AgentSession: &provider.AgentSession{Kind: "path"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Resolve(tt.input); !errors.Is(err, ErrNoSession) {
				t.Fatalf("Resolve(%#v) error = %v, want %v", tt.input, err, ErrNoSession)
			}
		})
	}
}

// The default is omp's own home, not pi's. A provider that defaulted to
// ~/.pi/agent would measure the wrong agent on any machine running both, and
// would do it silently: the two stores have the same layout below the home, so
// the same transcript is laid out under both here and only the right default
// resolves to omp's. Which home wins when one is configured is pinned once, in
// the provider root. Sets HOME, so it cannot run in parallel.
func TestResolveDefaultsToOmpsOwnHomeNotPis(t *testing.T) {
	fromDefault := t.TempDir()
	want := writeStore(t, filepath.Join(fromDefault, ".omp", "agent"), map[string][]string{repoSessionDir: {repoTranscript}})[repoTranscript]
	writeStore(t, filepath.Join(fromDefault, ".pi", "agent"), map[string][]string{repoSessionDir: {repoTranscript}})
	t.Setenv("HOME", fromDefault)

	got, err := Resolve(SessionInput{AgentSession: &provider.AgentSession{Kind: "id", Value: repoSessionID}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != want {
		t.Fatalf("Resolve() = %q, want the default ~/.omp/agent home %q", got.Path, want)
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
