package hermes

import (
	"context"
	"errors"
	"io/fs"
	"syscall"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

func TestStatesClassifyEveryAnswer(t *testing.T) {
	t.Parallel()

	states := New("").States()
	tests := []struct {
		name string
		err  error
		want report.Class
	}{
		{name: "unresolvable agent home", err: provider.ErrNoHome, want: report.Superseded},
		{name: "no reported session", err: ErrNoSession, want: report.Superseded},
		{name: "session ended", err: ErrSessionEnded, want: report.Superseded},
		{name: "store not written yet", err: ErrStoreNotReady, want: report.Superseded},
		{name: "session has produced no reading", err: ErrNoOccupancy, want: report.Superseded},
		{name: "just compacted", err: ErrJustCompacted, want: report.Superseded},
		// open turns a missing store into ErrStoreNotReady, so a raw missing-file
		// error here would mean the store taxonomy was bypassed.
		{name: "unexplained missing file", err: fs.ErrNotExist, want: report.Transient},
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

// Sets the environment, so it cannot run in parallel. Unsetting HOME defeats
// os.UserHomeDir only on unix.
func TestAnUnresolvableHomeSupersedesTheDisplayedValue(t *testing.T) {
	t.Setenv("HOME", "")

	p := New("")
	_, err := p.Resolve(context.Background(), provider.Pane{AgentSession: &provider.AgentSession{Kind: "id", Value: openID}})
	if !errors.Is(err, provider.ErrNoHome) {
		t.Fatalf("Resolve() error = %v, want it to wrap ErrNoHome", err)
	}
	if got := report.Classify(err, p.States()); got != report.Superseded {
		t.Fatalf("Classify(%v) = %v, want %v", err, got, report.Superseded)
	}
}
