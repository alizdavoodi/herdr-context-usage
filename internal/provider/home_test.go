package provider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomePrefersTheExplicitHome(t *testing.T) {
	t.Parallel()

	explicit := t.TempDir()
	got, err := Home(explicit, filepath.Join(".pi", "agent"))
	if err != nil || got != explicit {
		t.Fatalf("Home(explicit) = %q, %v; want %q", got, err, explicit)
	}
}

// The two cases below are the only reason this package's tests set the
// environment: the user's home directory is the one input Home reads for
// itself, so pinning the default means setting HOME. Unsetting HOME defeats
// os.UserHomeDir only on unix; Windows reads USERPROFILE.
func TestHomeJoinsTheDefaultUnderTheUsersHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := Home("", filepath.Join("x", "y"))
	if want := filepath.Join(home, "x", "y"); err != nil || got != want {
		t.Fatalf("Home(\"\") = %q, %v; want %q", got, err, want)
	}
}

func TestHomeWithoutAUserHomeWrapsErrNoHome(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := Home("", filepath.Join("x", "y"))
	if !errors.Is(err, ErrNoHome) {
		t.Fatalf("Home(\"\") error = %v, want it to wrap ErrNoHome", err)
	}
	// The sentinel is what providers classify on; the operating system's own
	// text is what a maintainer reads, so both have to survive the wrapping.
	if !strings.Contains(err.Error(), "$HOME") {
		t.Fatalf("Home(\"\") error = %q, want the operating system's diagnostic kept", err)
	}

	// A configured home must not depend on the host having one of its own: a
	// lookup that consulted the user's home first would blank every row a user
	// had pointed somewhere explicit.
	explicit := t.TempDir()
	if got, err := Home(explicit, filepath.Join("x", "y")); err != nil || got != explicit {
		t.Fatalf("Home(explicit) with no user home = %q, %v; want %q", got, err, explicit)
	}
}

// Both callers refuse an empty id before they get here, so this refusal is
// pinned directly: the pattern would become sessions/*/*.jsonl, and a store
// holding one transcript would resolve an empty id to it.
func TestSessionTranscriptRefusesAnEmptyID(t *testing.T) {
	t.Parallel()

	const id = "01a014de-020a-70b2-bc8e-b6a70ed3e784"
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "--Users-alirezadavoodi--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-08-18T12-34-48-971Z_"+id+".jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := SessionTranscript(home, id); !found || err != nil {
		t.Fatalf("SessionTranscript(id) = %t, %v; want the fixture found by its own id", found, err)
	}
	if path, found, err := SessionTranscript(home, ""); found || err != nil {
		t.Fatalf("SessionTranscript(\"\") = %q, %t, %v; want no match", path, found, err)
	}
}

// A glob that cannot run is a failure to look, not an answer about the session,
// so it comes back as an error rather than as no match: no match blanks the row,
// and a failure to look must leave the displayed value alone.
func TestSessionTranscriptReportsAGlobFailure(t *testing.T) {
	t.Parallel()

	if _, found, err := SessionTranscript("[", "01a014de"); found || !errors.Is(err, filepath.ErrBadPattern) {
		t.Fatalf("SessionTranscript() = %t, %v; want filepath.ErrBadPattern", found, err)
	}
}
