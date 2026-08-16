package report

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"
)

func TestSupersedesCurrentValue(t *testing.T) {
	t.Parallel()

	errUnknown := errors.New("unknown occupancy")
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "missing resolved resource", err: fmt.Errorf("open transcript: %w", fs.ErrNotExist), want: true},
		{name: "provider semantic state", err: errUnknown, want: true},
		{name: "wrapped provider semantic state", err: fmt.Errorf("read transcript: %w", errUnknown), want: true},
		{name: "permission failure", err: fs.ErrPermission, want: false},
		{name: "I/O failure", err: syscall.EIO, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SupersedesCurrentValue(tt.err, []error{errUnknown}); got != tt.want {
				t.Fatalf("SupersedesCurrentValue() = %v, want %v", got, tt.want)
			}
		})
	}
}
