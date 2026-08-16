package codex

import (
	"errors"
	"path/filepath"
	"testing"
)

// The compaction fixtures come from
// ~/.codex/sessions/2026/07/31/rollout-2026-07-31T17-43-45-019fb8d8-85b8-7c50-82ac-bc46ddf0d5fe.jsonl,
// a 595-line rollout that compacted once. The positions are the point: the
// pre-compaction token_count is line 396, the top-level `compacted` record is
// line 397, and the post-compaction token_count is line 400 — three
// milliseconds later, with a world_state and a turn_context record in between.
//
// Codex writes exactly one shape of compaction marker: a top-level
// {"type":"compacted"} record. Nothing in the 307-rollout corpus carries
// "compacted" as a payload type, so the reader's second detection position is
// unverified. Line 401 is {"payload":{"type":"context_compacted"}}, which sits
// after the post-compaction reading and must not match — if it did, every
// compacted session would report just-compacted forever.
func TestReadOccupancy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    int64
		wantErr error
	}{
		{
			// Lines 396, 397 and 400, with line 401 kept because it is the record
			// that proves the marker is matched by type and not by substring.
			// input_tokens is 0 after a compaction and total_tokens is 18162: the
			// difference between showing 7% full and 95% full.
			name:    "prefers total tokens after compaction",
			fixture: "compacted.jsonl",
			want:    18_162,
		},
		{
			// Line 594 of the same rollout with total_tokens deleted, which is the
			// only way to reach the fallback: all 307 rollouts in the corpus write
			// total_tokens on every last_token_usage. It stands in for a rollout
			// written by a codex old enough to omit it. The number also guards the
			// arithmetic: cached_input_tokens is a subset of input_tokens here, so
			// the answer is 85751 and never 85751 + 84736.
			name:    "falls back to input without adding cached input",
			fixture: "input-fallback.jsonl",
			want:    85_751,
		},
		{
			// A stand-in: no rollout in the corpus has ever recorded an all-zero
			// last_token_usage. It is line 594's record with every count zeroed,
			// standing in for a turn that failed before it consumed anything.
			name:    "all-zero usage is a failed turn, not an empty window",
			fixture: "zero-total.jsonl",
			wantErr: ErrFailedTurn,
		},
		{
			// Lines 396 and 397 with nothing after them: the file exactly as it
			// stood during the three milliseconds before line 400 was appended.
			// This is the instant the previous fixtures got wrong — the marker has
			// to be the last record, not merely present somewhere.
			name:    "just compacted is not an old occupancy",
			fixture: "just-compacted.jsonl",
			wantErr: ErrJustCompacted,
		},
		{
			// The same two records, then line 400's with every count zeroed. A
			// usage row that yields nothing must not end the just-compacted window:
			// the reading it would leave standing is 245,715, the size of the
			// context that was just thrown away.
			//
			// A stand-in, and doubly so: no rollout has an all-zero usage row at
			// all, and all 11 compaction markers in the corpus are followed by a
			// usage row carrying a real total, between 15,502 and 21,857.
			name:    "an empty usage row does not end the just-compacted window",
			fixture: "compacted-then-zero.jsonl",
			wantErr: ErrJustCompacted,
		},
		{
			// Line 396 alone, which is the file as it stood before the compaction
			// marker was written. Both numbers are present and they differ, so
			// this is what pins the preference: total_tokens, not input_tokens.
			// Every rollout writes both, so nothing else in the suite distinguishes
			// the two orders.
			name:    "prefers total tokens over input tokens",
			fixture: "prefers-total.jsonl",
			want:    245_715,
		},
		{
			// The whole of
			// ~/.codex/sessions/2026/01/29/rollout-2026-01-29T11-42-29-019c0958-ef52-7a83-b189-7f34e6ae0841.jsonl,
			// a four-record rollout whose session never took a turn: session_meta
			// and three response_items, no token_count anywhere.
			name:    "no token usage is unknown",
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

// The mirror of claude's TestReadOccupancyAddsInputAndBothCacheFields. Codex's
// cached_input_tokens is a subset of input_tokens, so adding the two roughly
// doubles the answer: 84,736 of 85,751 input tokens were cached in this
// measured turn, a 1.99x overstatement that is plausible enough to go
// unnoticed on a sidebar.
func TestReadOccupancyNeverAddsCachedInputToInput(t *testing.T) {
	t.Parallel()

	const (
		input  = 85_751
		cached = 84_736

		codexRule  = input          // 85,751
		claudeRule = input + cached // 170,487, a 1.99x overstatement
	)
	got, err := ReadOccupancy(filepath.Join("testdata", "input-fallback.jsonl"))
	if err != nil {
		t.Fatalf("ReadOccupancy() error = %v", err)
	}
	if got != codexRule {
		t.Fatalf("ReadOccupancy() = %d, want %d; %d would be claude's rule applied to codex's fields", got, codexRule, claudeRule)
	}
}

func TestReadOccupancyMissingRollout(t *testing.T) {
	t.Parallel()

	if _, err := ReadOccupancy(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("ReadOccupancy() error = nil")
	}
}
