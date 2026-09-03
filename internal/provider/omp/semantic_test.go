package omp

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

func TestSemanticStatesClassifyEveryAnswer(t *testing.T) {
	t.Parallel()

	states := SemanticStates()
	tests := []struct {
		name string
		err  error
		want report.Class
	}{
		{name: "unresolvable agent session", err: ErrNoSession, want: report.Superseded},
		{name: "no completed assistant turn", err: ErrNoOccupancy, want: report.Superseded},
		{name: "just compacted", err: ErrJustCompacted, want: report.Superseded},
		{
			// ReadOccupancy wraps the open error, so this is the shape the
			// classifier actually sees when a resolved transcript is gone.
			name: "resolved transcript does not exist",
			err:  fmt.Errorf("open omp transcript: %w", fs.ErrNotExist),
			want: report.Superseded,
		},
		{name: "unreadable transcript", err: fs.ErrPermission, want: report.Transient},
		{name: "I/O failure", err: syscall.EIO, want: report.Transient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := report.Classify(tt.err, states); got != tt.want {
				t.Fatalf("Classify(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// An unresolvable agent home is a permanent fact about the host, not a
// momentary failure: with no $HOME and no configured home there is nowhere to
// look, and every later refresh reads the same way. Left undeclared it would
// fall to Classify's transient default and freeze the displayed occupancy for
// the life of the pane, which presents a superseded number as current.
//
// This drives Resolve rather than a constructed error so the classification is
// pinned to the shape the provider actually produces. It sets the environment
// and therefore cannot run in parallel, and unsetting HOME defeats
// os.UserHomeDir only on unix — on Windows it reads USERPROFILE, so this trigger
// would need a second clause there. Only the id branch reaches a home;
// TestResolveAPathWithoutAResolvableHome pins the path branch staying healthy.
func TestAnUnresolvableHomeSupersedesTheDisplayedValue(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("HOME", "")

	_, err := Resolve(SessionInput{AgentSession: &AgentSession{Kind: "id", Value: "01a01485"}})
	if !errors.Is(err, ErrNoHome) {
		t.Fatalf("Resolve(id) error = %v, want it to wrap ErrNoHome", err)
	}
	// A reader diagnosing a blank row has to tell "there is no home to look in"
	// apart from "the agent session did not resolve"; they have different fixes.
	if errors.Is(err, ErrNoSession) {
		t.Fatalf("Resolve(id) error = %v, want it distinguishable from an unresolved agent session", err)
	}
	if got := report.Classify(err, SemanticStates()); got != report.Superseded {
		t.Fatalf("Classify(%v) = %v, want %v", err, got, report.Superseded)
	}
}
