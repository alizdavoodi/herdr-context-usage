package pi

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

func TestSupersededClassifiesEveryAnswer(t *testing.T) {
	t.Parallel()

	superseded := New("").Superseded()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "unresolvable agent session", err: ErrNoSession, want: true},
		{name: "unresolvable agent home", err: provider.ErrNoHome, want: true},
		{name: "no completed assistant turn", err: ErrNoOccupancy, want: true},
		{name: "just compacted", err: ErrJustCompacted, want: true},
		{
			// readOccupancy wraps the open error, so this is the shape the
			// classifier actually sees when a resolved transcript is gone.
			name: "resolved transcript does not exist",
			err:  fmt.Errorf("open pi transcript: %w", fs.ErrNotExist),
			want: true,
		},
		{name: "unreadable transcript", err: fs.ErrPermission, want: false},
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
// freeze the displayed occupancy for the life of the pane, which presents a
// superseded number as current.
//
// This drives resolve rather than a constructed error so the classification is
// pinned to the shape the provider actually produces. It sets the environment
// and therefore cannot run in parallel, and unsetting HOME defeats
// os.UserHomeDir only on unix — on Windows it reads USERPROFILE, so this trigger
// would need a second clause there. Only the id branch reaches a home;
// TestResolveAPathWithoutAResolvableHome pins the path branch staying healthy.
func TestAnUnresolvableHomeSupersedesTheDisplayedValue(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := resolve(sessionInput{AgentSession: &provider.AgentSession{Kind: "id", Value: "01a014de"}})
	if !errors.Is(err, provider.ErrNoHome) {
		t.Fatalf("resolve(id) error = %v, want it to wrap ErrNoHome", err)
	}
	// A reader diagnosing a blank row has to tell "there is no home to look in"
	// apart from "the agent session did not resolve"; they have different fixes.
	if errors.Is(err, ErrNoSession) {
		t.Fatalf("resolve(id) error = %v, want it distinguishable from an unresolved agent session", err)
	}
	if !provider.IsSuperseded(err, New("").Superseded()) {
		t.Fatalf("IsSuperseded(%v) = false, want true", err)
	}
}
