package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

func TestResolvePrefersReportedAgentSession(t *testing.T) {
	t.Parallel()

	reported := filepath.Join(t.TempDir(), "reported.jsonl")
	got, err := resolve(sessionInput{
		AgentSession:  &provider.AgentSession{Kind: "path", Value: reported},
		ForegroundCWD: "/ignored",
	})
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if got.Path != reported || got.Mode != provider.ResolutionAgentSession {
		t.Fatalf("resolve() = %#v, want reported path and agent_session mode", got)
	}
}

// A reported path is fully resolvable without a home, so a home that cannot be
// resolved at all must not sink it. Unsetting HOME with no configured home is
// the only way to make the home lookup fail, so this case sets the environment
// and cannot run in parallel.
func TestResolveAPathWithoutAResolvableHome(t *testing.T) {
	t.Setenv("HOME", "")

	reported := filepath.Join(t.TempDir(), "reported.jsonl")
	got, err := resolve(sessionInput{AgentSession: &provider.AgentSession{Kind: "path", Value: reported}})
	if err != nil {
		t.Fatalf("resolve() error = %v, want the reported path used without a home", err)
	}
	if got.Path != reported || got.Mode != provider.ResolutionAgentSession {
		t.Fatalf("resolve() = %#v, want %q and agent_session mode", got, reported)
	}
}

// herdr named a session for this pane, so a working-directory match could only
// disagree with it: a reported session this provider cannot use blanks the row
// rather than falling back. The sole transcript under the pane's directory is
// the bait, because the fallback would accept it.
func TestResolveBlanksAnUnusableReportedSession(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cwd := "/work/acme"
	bait := filepath.Join(home, "projects", "-work-acme", "only.jsonl")
	if err := os.MkdirAll(filepath.Dir(bait), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bait, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, session := range []provider.AgentSession{
		// A relative path would be read against the plugin's own working
		// directory, where a same-named file is not this pane's transcript.
		{Kind: "path", Value: "only.jsonl"},
		{Kind: "path", Value: "projects/-work-acme/only.jsonl"},
		{Kind: "path", Value: "./only.jsonl"},
		{Kind: "path", Value: "../only.jsonl"},
		// A label outside herdr's set is refused whatever its value looks like.
		{Kind: "unknown", Value: bait},
	} {
		got, err := resolve(sessionInput{ClaudeHome: home, ForegroundCWD: cwd, AgentSession: &session})
		if err != ErrNoSession {
			t.Fatalf("resolve(%#v) = %#v, %v; want ErrNoSession", session, got, err)
		}
	}
}

func TestResolveReportedSessionID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cwd := "/work/acme"
	want := filepath.Join(home, "projects", "-work-acme", "session-1.jsonl")
	// An unlabelled value that is not absolute is an id by the shared rule.
	for _, kind := range []string{"id", ""} {
		got, err := resolve(sessionInput{
			ClaudeHome:    home,
			AgentSession:  &provider.AgentSession{Kind: kind, Value: "session-1"},
			ForegroundCWD: cwd,
		})
		if err != nil {
			t.Fatalf("resolve(kind=%q) error = %v", kind, err)
		}
		if got.Path != want || got.Mode != provider.ResolutionAgentSession {
			t.Fatalf("resolve(kind=%q) = %#v, want %q and agent_session mode", kind, got, want)
		}
	}
}

// A reported id names a transcript under the pane's own project directory, so
// an id carrying a path separator would build a path somewhere else. The bait
// is another project's real transcript, which such an id reaches once joined.
func TestResolveRefusesAnIDThatLeavesThePaneDirectory(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	bait := filepath.Join(home, "projects", "-work-other", "session-1.jsonl")
	if err := os.MkdirAll(filepath.Dir(bait), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bait, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, session := range []provider.AgentSession{
		{Kind: "id", Value: "../-work-other/session-1"},
		{Value: "../-work-other/session-1"},
		{Kind: "id", Value: `..\-work-other\session-1`},
	} {
		got, err := resolve(sessionInput{ClaudeHome: home, ForegroundCWD: "/work/acme", AgentSession: &session})
		if err != ErrNoSession {
			t.Fatalf("resolve(%#v) = %#v, %v; want ErrNoSession", session, got, err)
		}
	}
}

func TestResolveFromForegroundCWDAndClaudeProcess(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cwd := "/work/acme"
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "sessions", "42.json"), []byte(`{"pid":42,"sessionId":"session-2","cwd":"/work/acme","kind":"interactive"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(home, "projects", "-work-acme", "session-2.jsonl")
	got, err := resolve(sessionInput{
		ClaudeHome:    home,
		ForegroundCWD: cwd,
		ProcessIDs:    []int{7, 42},
	})
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if got.Path != want || got.Mode != provider.ResolutionProcessRegistry {
		t.Fatalf("resolve() = %#v, want %q and process_registry mode", got, want)
	}
}

func TestResolveDoesNotGuessByRecency(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cwd := "/work/acme"
	projectDir := filepath.Join(home, "projects", "-work-acme")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"older.jsonl", "newer.jsonl"} {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := resolve(sessionInput{ClaudeHome: home, ForegroundCWD: cwd}); !errorsIsNoSession(err) {
		t.Fatalf("resolve() error = %v, want ErrNoSession", err)
	}
}

func TestResolveSingleTranscriptFromForegroundCWD(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cwd := "/work/only"
	want := filepath.Join(home, "projects", "-work-only", "only.jsonl")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolve(sessionInput{ClaudeHome: home, ForegroundCWD: cwd})
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if got.Path != want || got.Mode != provider.ResolutionForegroundCWD {
		t.Fatalf("resolve() = %#v", got)
	}
}

func TestResolveRejectsIncompleteInputs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	tests := []sessionInput{
		{ClaudeHome: home},
		{ClaudeHome: home, AgentSession: &provider.AgentSession{Kind: "id", Value: "session"}},
		{ClaudeHome: home, ForegroundCWD: "/work", AgentSession: &provider.AgentSession{Kind: "unknown", Value: "session"}},
	}
	for _, input := range tests {
		if _, err := resolve(input); err != ErrNoSession {
			t.Fatalf("resolve(%#v) error = %v, want ErrNoSession", input, err)
		}
	}
}

func errorsIsNoSession(err error) bool {
	return err == ErrNoSession
}
