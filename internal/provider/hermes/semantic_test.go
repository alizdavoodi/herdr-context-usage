package hermes

import (
	"context"
	"errors"
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
		{name: "unresolvable agent home", err: provider.ErrNoHome, want: true},
		{name: "no reported session", err: ErrNoSession, want: true},
		{name: "session ended", err: ErrSessionEnded, want: true},
		{name: "store not written yet", err: ErrStoreNotReady, want: true},
		{name: "session has produced no reading", err: ErrNoOccupancy, want: true},
		{name: "just compacted", err: ErrJustCompacted, want: true},
		// open turns a missing store into ErrStoreNotReady, so a raw missing-file
		// error here would mean the store taxonomy was bypassed.
		{name: "unexplained missing file", err: fs.ErrNotExist, want: false},
		{name: "unreadable database", err: fs.ErrPermission, want: false},
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

// Sets the environment, so it cannot run in parallel. Unsetting HOME defeats
// os.UserHomeDir only on unix.
func TestAnUnresolvableHomeSupersedesTheDisplayedValue(t *testing.T) {
	t.Setenv("HOME", "")

	p := New("")
	_, err := p.Measure(context.Background(), provider.Pane{AgentSession: &provider.AgentSession{Kind: "id", Value: openID}})
	if !errors.Is(err, provider.ErrNoHome) {
		t.Fatalf("Measure() error = %v, want it to wrap ErrNoHome", err)
	}
	if !provider.IsSuperseded(err, p.Superseded()) {
		t.Fatalf("IsSuperseded(%v) = false, want true", err)
	}
}
