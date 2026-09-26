package codex

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
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
		{name: "unresolvable session", err: ErrNoSession, want: report.Superseded},
		{name: "unresolvable agent home", err: provider.ErrNoHome, want: report.Superseded},
		{name: "no token usage recorded yet", err: ErrNoOccupancy, want: report.Superseded},
		{name: "turn recorded no usage", err: ErrFailedTurn, want: report.StaleLow},
		{name: "just compacted", err: ErrJustCompacted, want: report.Superseded},
		{
			// The rollout path is known before codex writes the file. Without this
			// declaration the window between the two would preserve the previous
			// session's number instead of clearing it.
			name: "rollout not written yet",
			err:  fmt.Errorf("open codex rollout: %w", fs.ErrNotExist),
			want: report.Superseded,
		},
		{name: "unreadable rollout", err: fs.ErrPermission, want: report.Transient},
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
// look, and every later refresh reads the same way. Left undeclared it fell to
// Classify's transient default and froze the displayed occupancy for the life
// of the pane, while pi and omp blanked theirs on the same host.
//
// This drives Resolve rather than a constructed error so the classification is
// pinned to the shape the provider actually produces. It sets the environment
// and therefore cannot run in parallel, and unsetting HOME defeats
// os.UserHomeDir only on unix — on Windows it reads USERPROFILE.
func TestAnUnresolvableHomeSupersedesTheDisplayedValue(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := Resolve(SessionInput{
		AgentSession:  &provider.AgentSession{Kind: "id", Value: "session-1"},
		ForegroundCWD: "/work",
	})
	if !errors.Is(err, provider.ErrNoHome) {
		t.Fatalf("Resolve(id) error = %v, want it to wrap ErrNoHome", err)
	}
	// A reader diagnosing a blank row has to tell "there is no home to look in"
	// apart from "the session did not resolve"; they have different fixes.
	if errors.Is(err, ErrNoSession) {
		t.Fatalf("Resolve(id) error = %v, want it distinguishable from an unresolved session", err)
	}
	if got := report.Classify(err, SemanticStates()); got != report.Superseded {
		t.Fatalf("Classify(%v) = %v, want %v", err, got, report.Superseded)
	}
}
