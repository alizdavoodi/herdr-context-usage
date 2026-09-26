package provider

import (
	"errors"
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
