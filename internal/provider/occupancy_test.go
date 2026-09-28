package provider

import (
	"math"
	"testing"
)

// Each case names the real record it came from, because the value of these
// fixtures is that both agents produced them independently: the same three
// addends, checked against two different oracles the two agents happen to write.
//
// pi 0.84.2, ~/.pi/agent/sessions/:
//
//	A line 39 of --Users-...-herdr-context-usage--/2026-08-18T12-34-48-971Z_01a014de-...jsonl
//	B line 68 of --Users-alirezadavoodi--/2026-08-16T20-45-15-107Z_01a00c52-...jsonl
//
// omp 17.3.5, ~/.omp/agent/sessions/-projects-github.com-alizdavoodi-herdr-context-usage/:
//
//	C line 250 and line 271 of 2026-08-18T10-57-53-412Z_01a01485-...jsonl
//	E line 6 of 2026-08-18T12-21-05-301Z_01a014d1-...jsonl
func TestOccupancySumsTheThreeAddends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                         string
		input, cacheRead, cacheWrite int64
		want                         int64
		wantOK                       bool
	}{
		{
			// pi A line 39, as written. Its own totalTokens is 30,458, which is
			// this sum plus the turn's output of 2,553.
			name: "pi mid-session turn", input: 769, cacheRead: 27_136, cacheWrite: 0,
			want: 27_905, wantOK: true,
		},
		{
			// omp C line 250, as written: the last turn before that transcript's
			// compaction. contextSnapshot.promptTokens is 132,098, exactly this
			// sum; totalTokens is 134,693, this sum plus the output of 2,595.
			name: "omp pre-compaction turn", input: 132_098, cacheRead: 0, cacheWrite: 0,
			want: 132_098, wantOK: true,
		},
		{
			// omp C line 271, one of only two omp rows with a non-zero cacheRead
			// — a deepseek turn rather than the ollama-cloud turns around it.
			// promptTokens is 44,552, so the cache count is an addend on omp too
			// and not merely on pi.
			name: "omp turn with a non-zero cacheRead", input: 2_312, cacheRead: 42_240, cacheWrite: 0,
			want: 44_552, wantOK: true,
		},
		{
			// Constructed, and the only case here that is: pi B line 68 with
			// cacheWrite raised from 0 to 3,500. cacheWrite is 0 on all 35 pi
			// rows and all 179 omp rows, so a turn writing cache has never been
			// captured on either agent. 796 + 39,680 + 3,500 is reachable from no
			// other combination in that record.
			name: "a cacheWrite that no real row carries", input: 796, cacheRead: 39_680, cacheWrite: 3_500,
			want: 43_976, wantOK: true,
		},
		{
			// omp E line 6, as written, and the reason this is not pi-only
			// reasoning: a real all-zero usage row, on a turn that also carries no
			// contextSnapshot. Reporting 0 here would read as an empty window; the
			// caller has to walk on to the previous completed turn instead.
			name: "an all-zero row yields no number", input: 0, cacheRead: 0, cacheWrite: 0,
			want: 0, wantOK: false,
		},
		{
			// Constructed: omp C line 250 with cacheRead negated. No count in
			// either store is negative. Without the guard this sums to 116,098 —
			// smaller than the truth, entirely plausible, and wrong.
			name: "a negative count yields no number", input: 132_098, cacheRead: -16_000, cacheWrite: 0,
			want: 0, wantOK: false,
		},
		{
			// Constructed: without the guard, math.MaxInt64 + 1 wraps to a
			// negative sum that would be reported as a reading.
			name: "a sum that overflows yields no number", input: math.MaxInt64, cacheRead: 1, cacheWrite: 0,
			want: 0, wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Occupancy(tt.input, tt.cacheRead, tt.cacheWrite)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("Occupancy(%d, %d, %d) = (%d, %t), want (%d, %t)",
					tt.input, tt.cacheRead, tt.cacheWrite, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
