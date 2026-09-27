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
