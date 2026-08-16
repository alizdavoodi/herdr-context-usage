package codex

import (
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
		{name: "unresolvable session", err: ErrNoSession, want: report.Superseded},
		{name: "no token usage recorded yet", err: ErrNoOccupancy, want: report.Superseded},
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
