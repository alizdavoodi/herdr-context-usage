package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every fixture here is derived from
// ~/.claude/projects/-Users-alirezadavoodi-projects-github-com-alizdavoodi-herdr-context-usage/6eb1e90f-04f1-4fa7-b553-9142da3f7913.jsonl
// unless a case says otherwise, with message content redacted and the usage
// objects kept as written. Real claude usage rows have a tiny input_tokens and
// carry the whole context in the two cache fields, which is why the numbers
// look lopsided and why summing all three is the only reading that can be
// right.
//
// The compaction fixtures are the exception and are marked as stand-ins: none
// of the 321 transcripts in the local corpus contains a compaction in any
// encoding — not isCompactSummary, not a compact_boundary system record, not a
// summary record — so the boundary this provider detects cannot be captured
// from a real store here.
func TestReadOccupancy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    int64
		wantErr error
	}{
		{
			// The usage records at lines 546 and 561, two successive assistant
			// turns fifteen records apart. Claude writes each usage row twice —
			// 546 and 547 are identical, as are 561 and 562 — and the fixture keeps
			// one of each pair, which is enough because the reader stops at the
			// first usable row it meets walking back.
			//
			// The newest wins, and its three fields are disjoint segments of one
			// prefix, so they add: 2 + 145977 + 3500. Reading input_tokens alone
			// would report 2.
			name:    "sums disjoint input and cache fields",
			fixture: "ordinary.jsonl",
			want:    149_479,
		},
		{
			// A splice, because the corpus contains no compaction to copy: line 561
			// as the turn before the boundary, a stand-in isCompactSummary record,
			// and then the usage record from line 20 — which in the real transcript
			// comes 541 records *earlier*, and is used here only because its
			// smaller numbers are what a first post-compaction turn looks like. The
			// position being asserted is the ordering inside this file, not
			// anything the transcript did.
			name:    "returns the lower post-compaction occupancy",
			fixture: "compacted.jsonl",
			want:    47_956,
		},
		{
			// Line 546, then the all-zero usage row from
			// ~/.claude/projects/-Users-alirezadavoodi--local-state-muxagent-worktrees-muxagent-coordinator-feature/09f971fe-9a29-480e-a7fc-b11d6cbafb3c.jsonl
			// line 7 of 8: an assistant record with isApiErrorMessage and "Credit
			// balance is too low". No transcript in the corpus contains both rows,
			// because the sessions that hit this error never produced a reading at
			// all; the pair is spliced to place the failure after a good turn.
			name:    "skips a synthetic zero usage entry after a failed turn",
			fixture: "rate-limited.jsonl",
			want:    145_979,
		},
		{
			// The same real record, alone in its session the way it actually
			// occurred: line 7 of 8, with no usable reading anywhere before it.
			name:    "zero-only usage is unknown",
			fixture: "zero-only.jsonl",
			wantErr: ErrNoOccupancy,
		},
		{
			// Line 561 followed by the stand-in compaction record with nothing
			// after it. The instant matters more than the shape: walking past the
			// boundary would report the pre-compaction 149479 as if it were current.
			name:    "does not walk past a just-compacted boundary",
			fixture: "just-compacted.jsonl",
			wantErr: ErrJustCompacted,
		},
		{
			// A prompt, a line the scanner cannot parse, and a turn_duration system
			// record — all three record types real, the truncation not. No
			// transcript contains a half-written record followed by whole ones; the
			// cut-off line stands in for the partial record the bounded tail begins
			// with once a transcript passes 8 MiB and the scan opens inside a large
			// one. What it pins is that an unparseable line is skipped rather than
			// ending the scan.
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

// Claude's and codex's cache fields are opposites, and both now live in one
// tree. Claude's three fields are disjoint segments of one prefix and add;
// codex's cached_input_tokens is a subset of input_tokens and must never be
// added. Applying codex's rule here reports 2 tokens for a 149k context, so
// this asserts the whole sum rather than only the total.
func TestReadOccupancyAddsInputAndBothCacheFields(t *testing.T) {
	t.Parallel()

	const (
		input         = 2
		cacheRead     = 145_977
		cacheCreation = 3_500

		claudeRule = input + cacheRead + cacheCreation // 149,479
		codexRule  = input                             // 2, if the cache fields are read as a subset
	)
	got, err := ReadOccupancy(filepath.Join("testdata", "ordinary.jsonl"))
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got != claudeRule {
		t.Fatalf("ReadOccupancy() = %d, want %d; %d would be codex's rule applied to claude's fields", got, claudeRule, codexRule)
	}
}

func TestReadOccupancyKeepsUsageBeyondTwoLargeRecords(t *testing.T) {
	t.Parallel()

	// Claude writes whole tool results into the transcript, and the largest
	// observed are around 600 KiB, so the bounded tail has to hold several of
	// them and still reach the usage record underneath.
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
