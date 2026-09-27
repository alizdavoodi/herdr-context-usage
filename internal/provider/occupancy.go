package provider

import "math"

// Occupancy sums the three counts describing the window a turn was sent with,
// and reports whether the row yields a number at all.
//
// The three are addends rather than subsets, and that is measured on pi and omp
// independently:
//
//   - pi 0.84.2 writes a per-turn totalTokens that equals this sum plus the
//     turn's output on all 35 usage rows of the two transcripts on this
//     machine, 33 of them with a non-zero cacheRead. That identity leaves no
//     room for a cache count to be a subset.
//   - omp 17.3.5 writes contextSnapshot.promptTokens, which equals this sum
//     exactly on all 178 turns carrying a snapshot across five transcripts,
//     including the two whose cacheRead is non-zero: 126 + 41,344 + 0 = 41,470
//     and 2,312 + 42,240 + 0 = 44,552. Its totalTokens satisfies pi's identity
//     on all 179 usage rows as well.
//
// cacheWrite is 0 on every real pi and omp row, so on those two agents it is the
// one addend resting on the identity rather than on a non-zero observation; each
// of their readers' tests pins it with a fixture labelled as constructed.
//
// None of the rows this rejects is an answer, which is why it returns a bool
// rather than a number. A negative count or a sum that overflows is a corrupt
// row. An all-zero row is a failed turn rather than an empty window, and no
// store examined has produced a genuine zero for a live session: claude writes
// one on rate-limit and billing errors, opencode v1 on an aborted turn, and omp
// on aborted and errored turns. Every reader walks on to the previous completed
// turn instead, because unknown is null and never zero.
//
// The negative and overflow guards are defensive, and that is why they live
// here once: deleting one where it is untested would turn a corrupt row into a
// plausible small number instead of no answer at all.
func Occupancy(input, cacheRead, cacheWrite int64) (int64, bool) {
	if input < 0 || cacheRead < 0 || cacheWrite < 0 ||
		input > math.MaxInt64-cacheRead || input+cacheRead > math.MaxInt64-cacheWrite {
		return 0, false
	}
	occupancy := input + cacheRead + cacheWrite
	return occupancy, occupancy != 0
}
