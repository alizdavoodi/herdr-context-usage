package codex

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
		{name: "unresolvable session", err: ErrNoSession, want: true},
		{name: "unresolvable agent home", err: provider.ErrNoHome, want: true},
		{name: "no token usage recorded yet", err: ErrNoOccupancy, want: true},
		{name: "turn recorded no usage", err: ErrFailedTurn, want: false},
		{name: "just compacted", err: ErrJustCompacted, want: true},
		{
			// The rollout path is known before codex writes the file. Without this
			// declaration the window between the two would preserve the previous
			// session's number instead of clearing it.
			name: "rollout not written yet",
			err:  fmt.Errorf("open codex rollout: %w", fs.ErrNotExist),
			want: true,
		},
		{name: "unreadable rollout", err: fs.ErrPermission, want: false},
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
// look, and every later refresh reads the same way. Left unclassified it would
// freeze the displayed occupancy for the life of the pane, presenting a
// superseded number as current.
//
// This drives resolve rather than a constructed error so the classification is
// pinned to the shape the provider actually produces. It sets the environment
// and therefore cannot run in parallel, and unsetting HOME defeats
// os.UserHomeDir only on unix — on Windows it reads USERPROFILE.
func TestAnUnresolvableHomeSupersedesTheDisplayedValue(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := resolve(sessionInput{
		AgentSession:  &provider.AgentSession{Kind: "id", Value: "session-1"},
		ForegroundCWD: "/work",
	})
	if !errors.Is(err, provider.ErrNoHome) {
		t.Fatalf("resolve(id) error = %v, want it to wrap ErrNoHome", err)
	}
	// A reader diagnosing a blank row has to tell "there is no home to look in"
	// apart from "the session did not resolve"; they have different fixes.
	if errors.Is(err, ErrNoSession) {
		t.Fatalf("resolve(id) error = %v, want it distinguishable from an unresolved session", err)
	}
	if !provider.IsSuperseded(err, New("").Superseded()) {
		t.Fatalf("IsSuperseded(%v) = false, want true", err)
	}
}
