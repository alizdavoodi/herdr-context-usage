package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrefersReportedAgentSession(t *testing.T) {
	t.Parallel()

	reported := filepath.Join(t.TempDir(), "reported.jsonl")
	got, err := Resolve(SessionInput{
		AgentSession:  &AgentSession{Kind: "path", Value: reported},
		ForegroundCWD: "/ignored",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != reported || got.Mode != ResolutionAgentSession {
		t.Fatalf("Resolve() = %#v, want reported path and agent_session mode", got)
	}
}

func TestResolveReportedSessionID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cwd := "/work/acme"
	want := filepath.Join(home, "projects", "-work-acme", "session-1.jsonl")
	got, err := Resolve(SessionInput{
		ClaudeHome:    home,
		AgentSession:  &AgentSession{Kind: "id", Value: "session-1"},
		ForegroundCWD: cwd,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != want || got.Mode != ResolutionAgentSession {
		t.Fatalf("Resolve() = %#v, want %q and agent_session mode", got, want)
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
	got, err := Resolve(SessionInput{
		ClaudeHome:    home,
		ForegroundCWD: cwd,
		ProcessIDs:    []int{7, 42},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != want || got.Mode != ResolutionProcessRegistry {
		t.Fatalf("Resolve() = %#v, want %q and process_registry mode", got, want)
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

	if _, err := Resolve(SessionInput{ClaudeHome: home, ForegroundCWD: cwd}); !errorsIsNoSession(err) {
		t.Fatalf("Resolve() error = %v, want ErrNoSession", err)
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

	got, err := Resolve(SessionInput{ClaudeHome: home, ForegroundCWD: cwd})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != want || got.Mode != ResolutionForegroundCWD {
		t.Fatalf("Resolve() = %#v", got)
	}
}

func TestResolveRejectsIncompleteInputs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	tests := []SessionInput{
		{ClaudeHome: home},
		{ClaudeHome: home, AgentSession: &AgentSession{Kind: "id", Value: "session"}},
		{ClaudeHome: home, ForegroundCWD: "/work", AgentSession: &AgentSession{Kind: "unknown", Value: "session"}},
	}
	for _, input := range tests {
		if _, err := Resolve(input); err != ErrNoSession {
			t.Fatalf("Resolve(%#v) error = %v, want ErrNoSession", input, err)
		}
	}
}

func errorsIsNoSession(err error) bool {
	return err == ErrNoSession
}
