// Package piompshared holds the one piece of arithmetic ADR-0006 permits pi and
// omp to share, and is deliberately hard to grow.
//
// ADR-0006 allows extraction only where the behaviour was measured identical on
// both agents, and refuses it everywhere else, because the two packages look
// near-identical at a glance and merging them will keep presenting itself as an
// obvious simplification. opencodeshared is the precedent for how much may live
// in a package like this one. What stays out, each because it was measured
// different rather than assumed so: the working-directory slug (pi wraps the
// whole absolute path, omp strips the home prefix), the per-turn usage shape (pi
// carries usage.reasoning and no contextSnapshot, omp carries
// contextSnapshot.promptTokens and nonMessageTokens and, on its deepseek turns,
// usage.reasoningTokens), the compaction record shape (pi's carries its own
// usage, omp's carries none), and the model-change record shape (pi writes
// provider plus modelId, omp writes model).
//
// A second exported name here would need its own measurement on both agents.
package piompshared

// Occupancy sums the three counts describing the window a turn was sent with,
// and reports whether the row yields a number at all.
//
// The three are addends rather than subsets, and that is measured on both
// agents independently:
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
// cacheWrite is 0 on every real row of both stores, so it is the one addend
// resting on the identity rather than on a non-zero observation; each provider's
// reader tests pin it with a fixture labelled as constructed.
//
// The two rejections are the reason this returns a bool rather than a number. A
// negative count has no precedent in either store and the guard against it is
// defensive, which is exactly why it is here rather than duplicated: deleting it
// where it is untested would turn a corrupt row into a plausible small number
// instead of no answer at all. An all-zero row is a failed turn rather than an
// empty window — omp writes a real one, and both agents' walks must reach the
// previous completed turn rather than report zero, because unknown is null and
// never zero.
func Occupancy(input, cacheRead, cacheWrite int64) (int64, bool) {
	if input < 0 || cacheRead < 0 || cacheWrite < 0 {
		return 0, false
	}
	occupancy := input + cacheRead + cacheWrite
	if occupancy == 0 {
		return 0, false
	}
	return occupancy, true
}
