package codex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePrefersReportedAgentSession(t *testing.T) {
	t.Parallel()

	reported := filepath.Join(t.TempDir(), "reported.jsonl")
	got, err := Resolve(SessionInput{AgentSession: &AgentSession{Kind: "path", Value: reported}, ForegroundCWD: "/ignored"})
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
	path := writeRollout(t, home, "session-1", "/work/old", nil, "user")
	got, err := Resolve(SessionInput{CodexHome: home, AgentSession: &AgentSession{Kind: "id", Value: "session-1"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != path || got.Mode != ResolutionAgentSession {
		t.Fatalf("Resolve() = %#v, want reported id path", got)
	}
}

func TestResolveFromForegroundCWDUsesActiveMainThread(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mainID := "main-session"
	parent := mainID
	path := writeRollout(t, home, mainID, "/work/acme", nil, "user")
	writeRollout(t, home, "sub-session", "/work/acme", &parent, "subagent")
	writeLock(t, home, mainID)
	writeLock(t, home, "sub-session")

	got, err := Resolve(SessionInput{CodexHome: home, ForegroundCWD: "/work/acme"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != path || got.Mode != ResolutionForegroundCWD {
		t.Fatalf("Resolve() = %#v, want active main-thread rollout", got)
	}
}

func TestResolveDoesNotGuessByRecency(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, home, "old-session", "/work/acme", nil, "user")
	writeRollout(t, home, "new-session", "/work/acme", nil, "user")

	if _, err := Resolve(SessionInput{CodexHome: home, ForegroundCWD: "/work/acme"}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Resolve() error = %v, want ErrNoSession", err)
	}
}

func TestResolveIgnoresLegacyRolloutsWithoutSessionMetadata(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	legacy := filepath.Join(home, "sessions", "2025", "08", "01", "rollout-legacy.jsonl")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"type":"event_msg","payload":{}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeRollout(t, home, "active-session", "/work/acme", nil, "user")
	writeLock(t, home, "active-session")

	got, err := Resolve(SessionInput{CodexHome: home, ForegroundCWD: "/work/acme"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Path != path {
		t.Fatalf("Resolve() = %#v, want active rollout", got)
	}
}

func TestResolveRejectsAmbiguousActiveSessions(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, home, "first", "/work/acme", nil, "user")
	writeRollout(t, home, "second", "/work/acme", nil, "user")
	writeLock(t, home, "first")
	writeLock(t, home, "second")

	if _, err := Resolve(SessionInput{CodexHome: home, ForegroundCWD: "/work/acme"}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Resolve() error = %v, want ErrNoSession", err)
	}
}

func writeRollout(t *testing.T, home, id, cwd string, parent *string, source string) string {
	t.Helper()
	path := filepath.Join(home, "sessions", "2026", "08", "16", "rollout-2026-08-16T12-00-00-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	parentJSON := "null"
	if parent != nil {
		parentJSON = `"` + *parent + `"`
	}
	entry := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `","parent_thread_id":` + parentJSON + `,"thread_source":"` + source + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeLock(t *testing.T, home, id string) {
	t.Helper()
	dir := filepath.Join(home, "thread-writer-locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
