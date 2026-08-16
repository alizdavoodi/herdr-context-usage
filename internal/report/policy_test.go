package report

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"
)

var (
	errNoSession  = errors.New("agent session cannot be resolved")
	errFailedTurn = errors.New("turn produced no usage")
)

func TestClassify(t *testing.T) {
	t.Parallel()

	states := States{Superseded: []error{errNoSession}, StaleLow: []error{errFailedTurn}}
	tests := []struct {
		name  string
		err   error
		state States
		want  Class
	}{
		{name: "declared superseded state", err: errNoSession, state: states, want: Superseded},
		{name: "wrapped superseded state", err: fmt.Errorf("read transcript: %w", errNoSession), state: states, want: Superseded},
		{name: "declared stale-low state", err: errFailedTurn, state: states, want: StaleLow},
		{name: "wrapped stale-low state", err: fmt.Errorf("read rollout: %w", errFailedTurn), state: states, want: StaleLow},
		{name: "permission failure", err: fs.ErrPermission, state: states, want: Transient},
		{name: "I/O failure", err: syscall.EIO, state: states, want: Transient},
		{
			// fs.ErrNotExist used to be special-cased here, which quietly gave codex
			// a superseded case it never declared. A provider that reads files by
			// path must now declare it itself.
			name:  "undeclared missing file",
			err:   fmt.Errorf("open transcript: %w", fs.ErrNotExist),
			state: states,
			want:  Transient,
		},
		{
			name:  "declared missing file",
			err:   fmt.Errorf("open transcript: %w", fs.ErrNotExist),
			state: States{Superseded: []error{fs.ErrNotExist}},
			want:  Superseded,
		},
		{
			// A provider that declares an answer twice has a bug; blanking the pane
			// is the safer of the two readings while it is found.
			name:  "contradictory declaration prefers superseded",
			err:   errNoSession,
			state: States{Superseded: []error{errNoSession}, StaleLow: []error{errNoSession}},
			want:  Superseded,
		},
		{name: "no declarations at all", err: errNoSession, state: States{}, want: Transient},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tt.err, tt.state); got != tt.want {
				t.Fatalf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}
