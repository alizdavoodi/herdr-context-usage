package opencode2

import (
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
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
		{name: "session has produced no reading", err: ErrNoOccupancy, want: report.Superseded},
		{name: "store not written yet", err: ErrStoreNotReady, want: report.Superseded},
		{name: "just compacted", err: ErrJustCompacted, want: report.Superseded},
		{name: "untitled session", err: opencodeshared.ErrUntitled, want: report.Superseded},
		{name: "pane reports no opencode title", err: opencodeshared.ErrTitleUnavailable, want: report.Superseded},
		{name: "terminal title disabled in this environment", err: opencodeshared.ErrTitleDisabled, want: report.Superseded},
		// Neither this nor the line above appears in SemanticStates. Both wrap the
		// unavailable-title answer that does, and both are listed so that a reader
		// adding a third attributed reason sees it has to keep that wrapping.
		{name: "mini interface", err: ErrMiniInterface, want: report.Superseded},
		{name: "plugin route title", err: opencodeshared.ErrPluginTitle, want: report.Superseded},
		{name: "title matches no single session", err: opencodeshared.ErrNoTitleMatch, want: report.Superseded},
		{
			// Open turns a missing database into ErrStoreNotReady, so a raw
			// missing-file error here would mean the store taxonomy was bypassed.
			name: "unexplained missing file",
			err:  fmt.Errorf("stat opencode2 store: %w", fs.ErrNotExist),
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
