package opencodeshared

import (
	"path/filepath"
	"testing"
)

func TestDataPathFollowsOpenCodesOwnLayout(t *testing.T) {
	t.Parallel()

	dataHome := t.TempDir()
	explicit := filepath.Join(t.TempDir(), "elsewhere.db")
	if got, err := DataPath(explicit, dataHome, "opencode.db"); err != nil || got != explicit {
		t.Fatalf("DataPath(explicit) = %q, %v; want the explicit path", got, err)
	}

	for _, filename := range []string{"opencode.db", "opencode2.db"} {
		got, err := DataPath("", dataHome, filename)
		if want := filepath.Join(dataHome, "opencode", filename); err != nil || got != want {
			t.Fatalf("DataPath(%q) = %q, %v; want %q", filename, got, err, want)
		}
	}
}
