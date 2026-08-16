package claude

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
		{
			name:    "sums disjoint input and cache fields",
			fixture: "ordinary.jsonl",
			want:    92_481,
		},
		{
			name:    "returns the lower post-compaction occupancy",
			fixture: "compacted.jsonl",
			want:    18_162,
		},
		{
			name:    "does not walk past a just-compacted boundary",
			fixture: "just-compacted.jsonl",
			wantErr: ErrNoOccupancy,
		},
		{
			name:    "no usable number is unknown",
			fixture: "no-usage.jsonl",
			wantErr: ErrNoOccupancy,
		},
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

func TestReadOccupancyMissingTranscript(t *testing.T) {
	t.Parallel()

	if _, err := ReadOccupancy(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("ReadOccupancy() error = nil")
	}
}
