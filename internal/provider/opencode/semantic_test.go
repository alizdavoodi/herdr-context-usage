package opencode

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
		{name: "untitled session", err: ErrUntitled, want: report.Superseded},
		{name: "session has produced no reading", err: ErrNoOccupancy, want: report.Superseded},
		{name: "database is not the v1 schema", err: ErrNoSchema, want: report.Superseded},
		{name: "store not written yet", err: ErrStoreNotReady, want: report.Superseded},
		{name: "just compacted", err: ErrJustCompacted, want: report.Superseded},
		{
			// Open turns a missing database into ErrStoreNotReady, so a raw
			// missing-file error here would mean the store taxonomy was bypassed.
			// Preserving the value is the right answer to something we cannot
			// explain.
			name: "unexplained missing file",
			err:  fmt.Errorf("stat opencode database: %w", fs.ErrNotExist),
			want: report.Transient,
		},
		{name: "unreadable database", err: fs.ErrPermission, want: report.Transient},
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
