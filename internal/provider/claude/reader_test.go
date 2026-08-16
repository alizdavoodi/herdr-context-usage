package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
			name:    "skips a synthetic zero usage entry after a failed turn",
			fixture: "rate-limited.jsonl",
			want:    129_000,
		},
		{
			name:    "zero-only usage is unknown",
			fixture: "zero-only.jsonl",
			wantErr: ErrNoOccupancy,
		},
		{
			name:    "does not walk past a just-compacted boundary",
			fixture: "just-compacted.jsonl",
			wantErr: ErrJustCompacted,
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

func TestReadOccupancyKeepsUsageBeyondTwoLargeRecords(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "large-tail.jsonl")
	usage := `{"type":"assistant","message":{"usage":{"input_tokens":1000,"cache_read_input_tokens":120000,"cache_creation_input_tokens":8000}}}` + "\n"
	large := `{"type":"user","message":{"content":"` + strings.Repeat("x", 700_000) + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(usage+large+large), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadOccupancy(path)
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got != 129_000 {
		t.Fatalf("ReadOccupancy() = %d, want 129000", got)
	}
}

func TestReadOccupancyMissingTranscript(t *testing.T) {
	t.Parallel()

	if _, err := ReadOccupancy(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("ReadOccupancy() error = nil")
	}
}
