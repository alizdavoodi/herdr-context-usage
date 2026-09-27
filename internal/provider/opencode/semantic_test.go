package opencode

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
)

func TestSupersededClassifiesEveryAnswer(t *testing.T) {
	t.Parallel()

	superseded := New("", "").Superseded()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "unresolvable session", err: ErrNoSession, want: true},
		{name: "unresolvable agent home", err: provider.ErrNoHome, want: true},
		{name: "session has produced no reading", err: ErrNoOccupancy, want: true},
		{name: "database is not the v1 schema", err: ErrNoSchema, want: true},
		{name: "store not written yet", err: ErrStoreNotReady, want: true},
		{name: "just compacted", err: ErrJustCompacted, want: true},
		{name: "untitled session", err: opencodeshared.ErrUntitled, want: true},
		{name: "pane reports no opencode title", err: opencodeshared.ErrTitleUnavailable, want: true},
		{name: "terminal title disabled in this environment", err: opencodeshared.ErrTitleDisabled, want: true},
		{name: "plugin route title", err: opencodeshared.ErrPluginTitle, want: true},
		{name: "title matches no single session", err: opencodeshared.ErrNoTitleMatch, want: true},
		{
			// open turns a missing database into ErrStoreNotReady, so a raw
			// missing-file error here would mean the store taxonomy was bypassed.
			// Preserving the value is the right answer to something we cannot
			// explain.
			name: "unexplained missing file",
			err:  fmt.Errorf("stat opencode database: %w", fs.ErrNotExist),
			want: false,
		},
		{name: "unreadable database", err: fs.ErrPermission, want: false},
		{name: "I/O failure", err: syscall.EIO, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := provider.IsSuperseded(tt.err, superseded); got != tt.want {
				t.Fatalf("IsSuperseded(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// An unresolvable agent home is a permanent fact about the host, not a
// momentary failure: with no $HOME and no configured home there is nowhere to
// look, and every later refresh reads the same way. Left undeclared it would
// freeze the displayed occupancy for the life of the pane, while pi and omp
// blank theirs on the same host.
//
// This drives open rather than a constructed error so the classification is
// pinned to the shape the provider actually produces. It sets the environment
// and therefore cannot run in parallel, and unsetting HOME defeats
// os.UserHomeDir only on unix — on Windows it reads USERPROFILE.
func TestAnUnresolvableHomeSupersedesTheDisplayedValue(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := open(context.Background(), "", "")
	if !errors.Is(err, provider.ErrNoHome) {
		t.Fatalf("open() error = %v, want it to wrap ErrNoHome", err)
	}
	// With no home there is no store path to stat, so this is not the store
	// being unwritten, and the two blank for different reasons.
	if errors.Is(err, ErrStoreNotReady) {
		t.Fatalf("open() error = %v, want it distinguishable from a store not written yet", err)
	}
	if !provider.IsSuperseded(err, New("", "").Superseded()) {
		t.Fatalf("IsSuperseded(%v) = false, want true", err)
	}
}
