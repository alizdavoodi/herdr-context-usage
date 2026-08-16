package codex

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestReadOccupancy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    int64
		wantErr error
	}{
		{name: "prefers total tokens after compaction", fixture: "compacted.jsonl", want: 18_162},
		{name: "falls back to input without adding cached input", fixture: "input-fallback.jsonl", want: 85_751},
		{name: "all-zero usage is unknown", fixture: "zero-total.jsonl", wantErr: ErrNoOccupancy},
		{name: "just compacted is not an old occupancy", fixture: "just-compacted.jsonl", wantErr: ErrJustCompacted},
		{name: "context compacted is not an old occupancy", fixture: "context-compacted.jsonl", wantErr: ErrJustCompacted},
		{name: "no token usage is unknown", fixture: "no-usage.jsonl", wantErr: ErrNoOccupancy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ReadOccupancy(filepath.Join("testdata", tt.fixture))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReadOccupancy() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ReadOccupancy() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadOccupancyMissingRollout(t *testing.T) {
	t.Parallel()

	if _, err := ReadOccupancy(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("ReadOccupancy() error = nil")
	}
}
