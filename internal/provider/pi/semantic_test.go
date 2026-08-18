package pi

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
		{name: "unresolvable agent session", err: ErrNoSession, want: report.Superseded},
		{name: "no completed assistant turn", err: ErrNoOccupancy, want: report.Superseded},
		{name: "just compacted", err: ErrJustCompacted, want: report.Superseded},
		{
			// ReadOccupancy wraps the open error, so this is the shape the
			// classifier actually sees when a resolved transcript is gone.
			name: "resolved transcript does not exist",
			err:  fmt.Errorf("open pi transcript: %w", fs.ErrNotExist),
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
