package provider

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"
)

func TestIsSuperseded(t *testing.T) {
	t.Parallel()

	// errNoSession stands in for a provider-local sentinel that is not one of
	// the shared roots. It deliberately carries the same text as ErrNoSession,
	// so the unrelated-error case below proves the classification is by
	// identity rather than by message.
	errNoSession := errors.New("agent session cannot be resolved")
	declared := []error{errNoSession}
	tests := []struct {
		name     string
		err      error
		declared []error
		want     bool
	}{
		{name: "declared answer", err: errNoSession, declared: declared, want: true},
		{name: "wrapped declared answer", err: fmt.Errorf("read transcript: %w", errNoSession), declared: declared, want: true},
		{name: "permission failure", err: fs.ErrPermission, declared: declared, want: false},
		{name: "I/O failure", err: syscall.EIO, declared: declared, want: false},
		{name: "undeclared missing file", err: fmt.Errorf("open transcript: %w", fs.ErrNotExist), declared: declared, want: false},
		{name: "declared missing file", err: fmt.Errorf("open transcript: %w", fs.ErrNotExist), declared: []error{fs.ErrNotExist}, want: true},
		{name: "no declarations at all", err: errNoSession, declared: nil, want: false},
		{name: "nil error", err: nil, declared: declared, want: false},
		// The four shared lifecycle outcomes supersede on their own, through an
		// extra wrapping layer and with no provider declaration at all.
		{name: "shared no-session root", err: ErrNoSession, declared: nil, want: true},
		{name: "wrapped shared no-session root", err: fmt.Errorf("resolve: %w", ErrNoSession), declared: nil, want: true},
		{name: "shared no-home root", err: ErrNoHome, declared: nil, want: true},
		{name: "wrapped shared no-home root", err: fmt.Errorf("resolve: %w", ErrNoHome), declared: nil, want: true},
		{name: "shared no-occupancy root", err: ErrNoOccupancy, declared: nil, want: true},
		{name: "wrapped shared no-occupancy root", err: fmt.Errorf("read: %w", ErrNoOccupancy), declared: nil, want: true},
		{name: "shared just-compacted root", err: ErrJustCompacted, declared: nil, want: true},
		{name: "wrapped shared just-compacted root", err: fmt.Errorf("read: %w", ErrJustCompacted), declared: nil, want: true},
		// An unrelated error spelling a shared root's exact words stays
		// undeclared: only identity reaches the classification.
		{name: "unrelated error with the no-session root's text", err: errors.New("agent session cannot be resolved"), declared: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsSuperseded(tt.err, tt.declared); got != tt.want {
				t.Fatalf("IsSuperseded(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
