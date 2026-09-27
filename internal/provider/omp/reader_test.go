package omp

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Provenance for every fixture in this file, measured against omp 17.3.5 and
// herdr 0.8.0. Five real transcripts exist on this machine, all under
// ~/.omp/agent/sessions/-projects-github.com-alizdavoodi-herdr-context-usage/,
// and three of them are used:
//
//	A = 2026-08-18T09-38-41-424Z_01a0143c-c290-7000-af1a-f1d510caa06d.jsonl
//	    191 lines, no compaction. Only line 8 is used: the one turn in this
//	    transcript whose nonMessageTokens sits above its prompt size.
//	C = 2026-08-18T10-57-53-412Z_01a01485-4504-7000-ae6b-ace797eca8f5.jsonl
//	    272 lines, the only compacted omp agent session available. Line 1 is the
//	    title record and line 2 the session record. Lines 247 and 250 are
//	    assistant turns with a toolResult and a custom record between them; line
//	    250 is the newest completed turn before the compaction, which is line
//	    252. Line 253 is the user record that reopened the agent session and line
//	    254 is the first post-compaction assistant turn. Line 271 is the newest
//	    turn in the file and the only one here on a different provider.
//	E = 2026-08-18T12-21-05-301Z_01a014d1-7095-7000-bca9-89174e6f99cd.jsonl
//	    9 lines. Only line 6 is used: a real all-zero usage row.
//
// Every usage and contextSnapshot object is copied exactly as written. The
// surrounding message fields are reduced — content, cost and details objects
// carry nothing this reader decodes — and the title record's pad and the
// compaction record's summary are abbreviated. Ids are kept where they were
// read.
//
// Position is what these fixtures are for. A record of the right shape at the
// wrong position measures the wrong instant, which is what both earlier codex
// regressions in this repository were, so each case below says where in A, C or
// E its records came from and in what order they are being replayed.
const (
	// C line 1, with pad abbreviated from 128 spaces. This is why omp's session
	// record is not the transcript's first line: the record is padded so omp can
	// rewrite the title in place. Its "v" is not a schema version and reading it
	// as one would report 1 for every omp agent session.
	titleC1 = `{"type":"title","v":1,"title":"Review q2, q5, q6, q7 feedback","source":"auto","updatedAt":"2026-08-18T11:13:14.011Z","pad":"    "}`

	// C line 2, as written. Every one of the five transcripts stamps version 3
	// at line 2, and pi stamps the same 3 at line 1 while carrying a different
	// field set — which is the whole reason the version is carried into the
	// output rather than gating anything.
	sessionC2 = `{"type":"session","version":3,"id":"01a01485-4504-7000-ae6b-ace797eca8f5","timestamp":"2026-08-18T10:57:53.412Z","cwd":"/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"}`

	// A stand-in: all five omp session records stamp version 3, as do both pi
	// ones, so no other value has been observed anywhere. That is the point of
	// the case it serves — the version must not gate the measurement.
	sessionV4 = `{"type":"session","version":4,"id":"01a01485-4504-7000-ae6b-ace797eca8f5","timestamp":"2026-08-18T10:57:53.412Z","cwd":"/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"}`

	// C line 1 with a version key added beside its own "v". A stand-in with no
	// precedent: only the session record carries a version in any transcript. It
	// exists because without it the type check in readVersion cannot be shown to
	// do anything — every other non-session header record lacks a version, so the
	// version comes back from the session record whether the type is checked or
	// not, and a case that reads as though it covers the check passes with the
	// check deleted.
	versionedTitleRecord = `{"type":"title","v":1,"version":9,"title":"Review q2, q5, q6, q7 feedback","source":"auto","updatedAt":"2026-08-18T11:13:14.011Z","pad":"    "}`

	// C lines 3 and 4, the bookkeeping records omp writes when the model or the
	// thinking level changes. They carry no message field at all, and they are
	// not rare: transcript D's first seven lines are the title record, the
	// session record, and then five of these — three model_change and two
	// thinking_level_change — with its first message only at line 8. omp names
	// the model in one "model" field where pi writes "provider" plus "modelId",
	// one of the divergences that keeps the two readers apart.
	modelChangeC3         = `{"type":"model_change","id":"5742fe73","parentId":null,"timestamp":"2026-08-18T10:57:53.458Z","model":"ollama-cloud/glm-5.2","resolvedModelIsFallback":false}`
	thinkingLevelChangeC4 = `{"type":"thinking_level_change","id":"a7184f32","parentId":"5742fe73","timestamp":"2026-08-18T10:57:53.458Z","thinkingLevel":"high","configured":null}`

	// C line 251, a custom record. omp writes 92 of these in this transcript
	// alone and they carry no message field either. This one records the agent
	// session exiting, and it sits immediately before the compaction record,
	// which is why it is worth having in the fixture set: a walk that stopped at
	// the newest record it could not classify would never reach the compaction.
	customC251 = `{"type":"custom","customType":"session_exit","data":{"reason":"dispose","kind":"normal","recordedAt":"2026-08-18T11:15:33.801Z"},"id":"7f357a96","parentId":"8a99ff86","timestamp":"2026-08-18T11:15:33.801Z"}`

	// C line 246, the toolResult between the two turns, content and details
	// dropped. Every user and toolResult record in all five transcripts carries
	// no usage.
	toolResultC246 = `{"type":"message","id":"327de39a","parentId":"167efdc3","timestamp":"2026-08-18T11:14:20.756Z","message":{"role":"toolResult","toolCallId":"ollama:0:bash","toolName":"bash","isError":false,"timestamp":1787051660756}}`

	// C line 253, the user record that reopened the agent session after the
	// compaction.
	userC253 = `{"type":"message","id":"706f5ec7","parentId":"6a93e330","timestamp":"2026-08-18T12:27:14.473Z","message":{"role":"user","attribution":"user","timestamp":1787056034430}}`

	// C line 247: 130869 + 0 + 0 = 130869, and contextSnapshot.promptTokens
	// agrees. It exists so the newest pre-compaction turn has something older to
	// beat.
	turnC247 = `{"type":"message","id":"d9f8e822","parentId":"327de39a","timestamp":"2026-08-18T11:14:28.969Z","message":{"role":"assistant","api":"ollama-chat","provider":"ollama-cloud","model":"glm-5.2","usage":{"input":130869,"output":431,"cacheRead":0,"cacheWrite":0,"totalTokens":131300},"stopReason":"toolUse","timestamp":1787051660782,"contextSnapshot":{"promptTokens":130869,"nonMessageTokens":19016}}}`

	// C line 250, the newest completed turn before C's compaction: 132098 + 0 +
	// 0 = 132098, which promptTokens confirms. Its totalTokens of 134693 is that
	// sum plus the turn's output of 2595, and the compaction record below repeats
	// 134693 as its tokensBefore — the two numbers that must never be read are
	// both present in the fixture set on purpose, and they are the same number.
	turnC250 = `{"type":"message","id":"8a99ff86","parentId":"b8d7c3d9","timestamp":"2026-08-18T11:15:01.800Z","message":{"role":"assistant","api":"ollama-chat","provider":"ollama-cloud","model":"glm-5.2","usage":{"input":132098,"output":2595,"cacheRead":0,"cacheWrite":0,"totalTokens":134693},"stopReason":"stop","timestamp":1787051669462,"contextSnapshot":{"promptTokens":132098,"nonMessageTokens":19016}}}`

	// C line 252, the compaction record, summary and shortSummary abbreviated and
	// details reduced to its shape. It carries no usage of its own, which is the
	// divergence from pi that makes a single reader impossible: a walk stopping
	// at the newest record holding a usage is correct on omp and silently wrong
	// on pi. tokensBefore is kept because it is the trap — 134693 is line 250's
	// per-turn total, describing the agent session before the compaction.
	compactionC252 = `{"type":"compaction","id":"6a93e330","parentId":"7f357a96","timestamp":"2026-08-18T12:22:29.244Z","summary":"## Goal ...","shortSummary":"I c...","firstKeptEntryId":"12d03f0e","tokensBefore":134693,"details":{"readFiles":[],"modifiedFiles":[]},"fromExtension":false}`

	// C line 254, the first assistant turn after C's compaction: 40710 + 0 + 0 =
	// 40710, against 132098 immediately before it. The breakdown is not zeroed
	// and carries no marker of the compaction — input simply carries the whole
	// re-sent summary — so the ordinary arithmetic is all that is needed once the
	// walk stops at line 252.
	turnC254 = `{"type":"message","id":"32a5030c","parentId":"706f5ec7","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","api":"ollama-chat","provider":"ollama-cloud","model":"glm-5.2","usage":{"input":40710,"output":38,"cacheRead":0,"cacheWrite":0,"totalTokens":40748},"stopReason":"stop","timestamp":1787056034465,"contextSnapshot":{"promptTokens":40710,"nonMessageTokens":18981}}}`

	// C line 271, the newest turn in the file and one of only two rows across all
	// five transcripts with a non-zero cacheRead — a deepseek turn among
	// ollama-cloud ones. 2312 + 42240 + 0 = 44552, which promptTokens confirms:
	// the cache count is an addend on omp and not merely on pi. It also carries
	// reasoningTokens, which is omp's name for a field pi calls reasoning, and
	// neither is read.
	turnC271 = `{"type":"message","id":"b997df0a","parentId":"8688776a","timestamp":"2026-08-18T12:58:00.073Z","message":{"role":"assistant","api":"openai-completions","provider":"deepseek","model":"deepseek-v4-flash","usage":{"input":2312,"output":931,"cacheRead":42240,"cacheWrite":0,"totalTokens":45483,"reasoningTokens":557},"stopReason":"stop","timestamp":1787057869087,"responseId":"9e3888bb-afcb-4df5-a843-79191406a9c3","contextSnapshot":{"promptTokens":44552,"nonMessageTokens":18016}}}`

	// A line 8, and the reason nonMessageTokens is not decoded at all: 48928 + 0
	// + 0 = 48928, while nonMessageTokens on the same record is 50213 — above the
	// prompt size rather than inside it. Four such rows exist across A and B
	// against 174 where it sits below, so no subset relation holds in either
	// direction and the field can be neither added nor subtracted.
	turnA8 = `{"type":"message","id":"ae6cfe54","parentId":"56aa24bb","timestamp":"2026-08-18T09:42:51.304Z","message":{"role":"assistant","api":"ollama-chat","provider":"ollama-cloud","model":"glm-5.2","usage":{"input":48928,"output":75,"cacheRead":0,"cacheWrite":0,"totalTokens":49003},"stopReason":"toolUse","timestamp":1787046167292,"contextSnapshot":{"promptTokens":48928,"nonMessageTokens":50213}}}`

	// E line 6, as written: a real all-zero usage row on a turn the user
	// interrupted. It carries no contextSnapshot at all, which is why an absent
	// snapshot cannot be treated as an oracle disagreement — omp declines to
	// describe a turn that consumed nothing.
	turnE6AllZero = `{"type":"message","id":"fbfb8d78","parentId":"27a3b45f","timestamp":"2026-08-18T12:21:15.624Z","message":{"role":"assistant","api":"ollama-chat","provider":"ollama-cloud","model":"deepseek-v4-flash","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0},"stopReason":"aborted","errorMessage":"Interrupted by user","errorId":67112960,"timestamp":1787055675620}}`

	// C line 254 with the two cache keys deleted. A stand-in: all 179 usage rows
	// across the five transcripts write all four counts, so an absent key has no
	// precedent. The answer is unchanged at 40710, which is what makes it worth
	// pinning — an absent count must read as zero rather than as an unusable row.
	turnC254NoCacheKeys = `{"type":"message","id":"32a5030c","parentId":"706f5ec7","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","usage":{"input":40710,"output":38,"totalTokens":40748},"stopReason":"stop","contextSnapshot":{"promptTokens":40710,"nonMessageTokens":18981}}}`

	// C line 254 with cacheRead negated. A stand-in with no precedent anywhere:
	// no count in any transcript is negative and omp has no reason to write one.
	// The guard against it is therefore defensive, which is exactly why it needs
	// a case — an untested guard can be deleted without anything noticing, and
	// its deletion would turn a corrupt row into a plausible small number rather
	// than into no answer at all.
	turnC254NegativeCache = `{"type":"message","id":"32a5030c","parentId":"706f5ec7","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","usage":{"input":40710,"output":38,"cacheRead":-16000,"cacheWrite":0,"totalTokens":40748},"stopReason":"stop"}}`

	// C line 254 with its usage object removed, standing in for an assistant
	// record whose turn has not completed. No such record exists in any
	// transcript — every one of the 179 assistant records carries a usage —
	// because omp writes the record once the turn is done.
	turnC254NoUsage = `{"type":"message","id":"32a5030c","parentId":"706f5ec7","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","stopReason":"stop","timestamp":1787056034465}}`

	// C line 254 with its contextSnapshot removed and the counts left alone. A
	// stand-in: the only real row without a snapshot is E line 6, which is
	// all-zero and never reaches the oracle. It stands in for a turn omp measured
	// but declined to describe, and the answer must still be the sum.
	turnC254NoSnapshot = `{"type":"message","id":"32a5030c","parentId":"706f5ec7","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","usage":{"input":40710,"output":38,"cacheRead":0,"cacheWrite":0,"totalTokens":40748},"stopReason":"stop"}}`

	// C line 254 with contextSnapshot present but promptTokens removed. A
	// stand-in: all 178 real snapshots carry the key, so this shape has no
	// precedent — but the oracle exists precisely to notice omp changing its
	// usage fields, and dropping one key from a record it still writes is the
	// cheapest change it could make. Without the nil check the deref panics, and
	// a panic here is worse than any wrong number: nothing in this plugin
	// recovers, so the whole refresh dies and every pane in that round loses its
	// update. The negative-count guard next door earned a fixture for a milder
	// failure than this one.
	turnC254SnapshotWithoutPromptTokens = `{"type":"message","id":"32a5030c","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","usage":{"input":40710,"output":38,"cacheRead":0,"cacheWrite":0,"totalTokens":40748},"stopReason":"stop","contextSnapshot":{"nonMessageTokens":18981}}}`

	// C line 254 with promptTokens raised by 1200 and the counts left alone. A
	// stand-in with no precedent: the two agreed exactly on all 178 real turns
	// carrying a snapshot. It stands in for a future omp release whose usage
	// fields stop summing to its own prompt size, and it is the guard against a
	// later reader quietly promoting the oracle to the source.
	turnC254OracleDisagrees = `{"type":"message","id":"32a5030c","parentId":"706f5ec7","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","usage":{"input":40710,"output":38,"cacheRead":0,"cacheWrite":0,"totalTokens":40748},"stopReason":"stop","contextSnapshot":{"promptTokens":41910,"nonMessageTokens":18981}}}`

	// C line 250 with cacheWrite raised from 0 to 3500 and totalTokens and
	// promptTokens raised to match, so the fixture still satisfies both of omp's
	// own identities. A stand-in for that one field: cacheWrite is 0 on all 179
	// real omp rows and all 35 real pi rows, so a turn writing cache has never
	// been captured on either agent. The expected 132098 + 0 + 3500 = 135598 is
	// reachable from no other combination in the record.
	turnC250CacheWrite = `{"type":"message","id":"8a99ff86","parentId":"b8d7c3d9","timestamp":"2026-08-18T11:15:01.800Z","message":{"role":"assistant","usage":{"input":132098,"output":2595,"cacheRead":0,"cacheWrite":3500,"totalTokens":138193},"stopReason":"stop","contextSnapshot":{"promptTokens":135598,"nonMessageTokens":19016}}}`

	// A splice with no real precedent: C line 246's toolResult carrying C line
	// 250's usage and snapshot. Only assistant records carry a usage in any
	// transcript, so this exists purely to keep the role half of the gate
	// load-bearing — without it, gating on the presence of a usage alone would
	// pass every other case here, and a usage appearing on some other role later
	// would silently be read as a completed turn.
	toolResultWithUsage = `{"type":"message","id":"327de39a","parentId":"167efdc3","timestamp":"2026-08-18T11:14:20.756Z","message":{"role":"toolResult","toolCallId":"ollama:0:bash","toolName":"bash","isError":false,"usage":{"input":132098,"output":2595,"cacheRead":0,"cacheWrite":0,"totalTokens":134693},"contextSnapshot":{"promptTokens":132098,"nonMessageTokens":19016}}}`

	// Not from any transcript: a line that cannot be parsed at all.
	malformedLine = `{"type":"message","message":{"role":"assistant","usage":{"input":`
)

func TestReadOccupancy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		records     []string
		want        int64
		wantVersion *int
		wantErr     error
	}{
		{
			// C lines 1, 2, 247, 246 and 250 in their real order. The newest wins.
			name:        "sums the newest completed assistant turn",
			records:     []string{titleC1, sessionC2, turnC247, toolResultC246, turnC250},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			// C line 271, where the cache count carries most of the window: 2312
			// + 42240 + 0, not the 2312 that reading input alone would report and
			// not the 45483 its own total carries.
			name:        "adds input and a non-zero cache read",
			records:     []string{titleC1, sessionC2, turnC271},
			want:        44_552,
			wantVersion: intPtr(3),
		},
		{
			name:        "an absent cache count reads as zero",
			records:     []string{titleC1, sessionC2, turnC254NoCacheKeys},
			want:        40_710,
			wantVersion: intPtr(3),
		},
		{
			name:        "adds a cache write",
			records:     []string{titleC1, sessionC2, turnC250CacheWrite},
			want:        135_598,
			wantVersion: intPtr(3),
		},
		{
			// C lines 1, 2, 250, 251 and 252: the file exactly as it stood between
			// the compaction and the next turn. Walking past line 252 would report
			// 132098, the size of the context that had just been thrown away.
			name:        "stops at a compaction record and reports the version",
			records:     []string{titleC1, sessionC2, turnC250, customC251, compactionC252},
			wantVersion: intPtr(3),
			wantErr:     ErrJustCompacted,
		},
		{
			// C lines 1, 2, 250, 251, 252, 253 and 254 in their real order. The
			// walk stops at the newest compaction, so once line 254 exists it is
			// the answer, and the pre-compaction 132098 behind line 252 is
			// unreachable without any compensating arithmetic.
			name:        "measures the completed turn after a compaction",
			records:     []string{titleC1, sessionC2, turnC250, customC251, compactionC252, userC253, turnC254},
			want:        40_710,
			wantVersion: intPtr(3),
		},
		{
			name:        "skips an assistant record whose turn has not completed",
			records:     []string{titleC1, sessionC2, turnC250, turnC254NoUsage},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			// E line 6 replayed after C line 250, which is not their real order:
			// what is being pinned is that a real interrupted turn does not report
			// zero. Unknown is null and never zero, so the walk reaches the
			// previous completed turn instead.
			name:        "skips a real all-zero usage row rather than reporting zero",
			records:     []string{titleC1, sessionC2, turnC250, turnE6AllZero},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			// Without the guard this row sums to 24,710 and is reported as the
			// current occupancy — small, plausible, and wrong. Skipping it reaches
			// the previous completed turn instead.
			name:        "skips a row carrying a negative count",
			records:     []string{titleC1, sessionC2, turnC250, turnC254NegativeCache},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			name:        "an unparseable line does not mask an earlier turn",
			records:     []string{titleC1, sessionC2, turnC250, malformedLine},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			// C lines 246 and 253 replayed after line 250, which is not their real
			// order: what is being pinned is that neither role ends the walk.
			name:        "skips user and toolResult records",
			records:     []string{titleC1, sessionC2, turnC250, toolResultC246, userC253},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			// C lines 3, 4 and 251 replayed after line 250, which is not where
			// they occur: what is being pinned is that a record carrying no
			// message field at all does not end the walk. omp writes far more of
			// these than pi does — 92 custom records in C alone.
			name:        "skips bookkeeping records that carry no message",
			records:     []string{titleC1, sessionC2, turnC250, modelChangeC3, thinkingLevelChangeC4, customC251},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			// C line 247 followed by the spliced toolResult carrying line 250's
			// usage. The answer is line 247's 130869; 132098 would mean the walk
			// had accepted a usage on a role that is not an assistant turn.
			name:        "a usage on a role that is not an assistant turn is not a turn",
			records:     []string{titleC1, sessionC2, turnC247, toolResultWithUsage},
			want:        130_869,
			wantVersion: intPtr(3),
		},
		{
			// C lines 1, 2, 253 and 246 with no assistant record anywhere, which
			// is what an agent session that has not completed a turn looks like.
			// Unknown is an error rather than a zero.
			name:        "a transcript with no assistant turn is unknown",
			records:     []string{titleC1, sessionC2, userC253, toolResultC246},
			wantVersion: intPtr(3),
			wantErr:     ErrNoOccupancy,
		},
		{
			name:        "measures at an unexpected schema version and reports it",
			records:     []string{titleC1, sessionV4, turnC250},
			want:        132_098,
			wantVersion: intPtr(4),
		},
		{
			name:    "a missing session record leaves the version unknown",
			records: []string{titleC1, turnC250},
			want:    132_098,
		},
		{
			// The session record is line 2, so the search has to walk past line 1
			// rather than read it. A reader that took the first line would find no
			// version on any real omp transcript.
			name:        "finds the session record behind the title record",
			records:     []string{titleC1, sessionC2, turnC250},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			// The case above cannot fail on its own: line 1 carries no version, so
			// the version comes from the session record whether or not the type is
			// checked. This one puts a version on the title record, which is the
			// only shape that tells the two apart — and it is the shape omp very
			// nearly writes already, its "v" sitting where a careless reader would
			// look.
			name:        "a version on a record that is not the session record is not read",
			records:     []string{versionedTitleRecord, sessionC2, turnC250},
			want:        132_098,
			wantVersion: intPtr(3),
		},
		{
			name:        "an unparseable header line does not hide the session record",
			records:     []string{malformedLine, sessionC2, turnC250},
			want:        132_098,
			wantVersion: intPtr(3),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := readOccupancy(writeTranscript(t, tt.records...))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("readOccupancy() error = %v, want %v", err, tt.wantErr)
			}
			if got.Occupancy != tt.want {
				t.Fatalf("readOccupancy() occupancy = %d, want %d", got.Occupancy, tt.want)
			}
			if !sameVersion(got.TranscriptVersion, tt.wantVersion) {
				t.Fatalf("readOccupancy() version = %s, want %s", formatVersion(got.TranscriptVersion), formatVersion(tt.wantVersion))
			}
		})
	}
}

// The whole point of the omp provider, and it fails in the opposite direction
// from pi's. omp's compaction record carries no usage of its own, so a walk that
// stopped at the newest record holding a usage would sail past C line 252
// entirely and report C line 250's 132098 — a nearly full window for an agent
// session whose true occupancy one turn later was 40710. Both numbers are named
// here so that such a change fails this test rather than shipping.
func TestReadOccupancyNeverWalksPastTheCompactionRecord(t *testing.T) {
	t.Parallel()

	const (
		lastPreCompactionTurn = 132_098 // C line 250's input + cacheRead + cacheWrite
		trueOccupancyAfter    = 40_710  // C line 254, the next turn boundary
		measuredOverstatement = 3.2     // 132098 / 40710 = 3.24
	)

	got, err := readOccupancy(writeTranscript(t, titleC1, sessionC2, turnC250, customC251, compactionC252))
	// The overstatement is asserted before the error, so that a walk which
	// sailed past the compaction record fails naming the number it reached
	// rather than only the sentinel it did not return.
	if got.Occupancy == lastPreCompactionTurn {
		t.Fatalf("readOccupancy() = %d, the last pre-compaction turn; that is %.1fx the %d this agent session actually held one turn later, and the walk must stop on the record type",
			got.Occupancy, measuredOverstatement, trueOccupancyAfter)
	}
	if got.Occupancy != 0 {
		t.Fatalf("readOccupancy() = %d, want no occupancy at a compaction boundary", got.Occupancy)
	}
	if !errors.Is(err, ErrJustCompacted) {
		t.Fatalf("readOccupancy() error = %v, want %v", err, ErrJustCompacted)
	}
}

// omp's own record of the prompt size is an oracle, never the source. Agreement
// is the measured case — all 178 real turns carrying a snapshot — and a
// disagreement has to reach the caller as a diagnostic while the sum is still
// what gets reported. This is the guard against a later reader deciding the
// field is simpler to read than the sum is to compute.
func TestReadOccupancyTreatsThePromptSizeAsAnOracle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		record                 string
		want                   int64
		wantDisagreementNaming []string
	}{
		{
			// C line 250, as written: promptTokens 132098 against a sum of 132098.
			name:   "agreement is silent",
			record: turnC250,
			want:   132_098,
		},
		{
			// C line 271, as written, and the case agreement is worth most on:
			// promptTokens 44552 against 2312 + 42240 + 0, where a reader that had
			// dropped the cache count would be off by 42240 and this would say so.
			name:   "agreement holds where the cache count carries the window",
			record: turnC271,
			want:   44_552,
		},
		{
			// The stand-in. The sum is still reported, and both numbers appear in
			// the diagnostic so a maintainer can see which way omp moved.
			name:                   "a disagreement is recorded and the sum still reported",
			record:                 turnC254OracleDisagrees,
			want:                   40_710,
			wantDisagreementNaming: []string{"41910", "40710"},
		},
		{
			// An absent snapshot is not a disagreement: omp declines to describe
			// some turns, and silence is not evidence against the arithmetic.
			name:   "an absent snapshot is not a disagreement",
			record: turnC254NoSnapshot,
			want:   40_710,
		},
		{
			// A snapshot carrying no promptTokens is the same silence one level
			// down, and it is the shape that panics without the nil check rather
			// than merely reporting the wrong thing.
			name:   "a snapshot without a prompt size is not a disagreement",
			record: turnC254SnapshotWithoutPromptTokens,
			want:   40_710,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := readOccupancy(writeTranscript(t, titleC1, sessionC2, tt.record))
			if err != nil {
				t.Fatalf("readOccupancy() error = %v", err)
			}
			if got.Occupancy != tt.want {
				t.Fatalf("readOccupancy() = %d, want the sum %d reported whatever the oracle says", got.Occupancy, tt.want)
			}
			if len(tt.wantDisagreementNaming) == 0 {
				if got.OracleDisagreement != "" {
					t.Fatalf("readOccupancy() disagreement = %q, want none", got.OracleDisagreement)
				}
				return
			}
			for _, number := range tt.wantDisagreementNaming {
				if !strings.Contains(got.OracleDisagreement, number) {
					t.Fatalf("readOccupancy() disagreement = %q, want it to name %s", got.OracleDisagreement, number)
				}
			}
		})
	}
}

// omp's own per-turn total is a second oracle for the arithmetic, and it is the
// one pi also writes: on all 179 usage rows across the five transcripts, input +
// cacheRead + cacheWrite + output equals totalTokens, which leaves no room for a
// cache count to be a subset. So the occupancy has to come out at totalTokens
// minus that turn's output, and a reader that reported the total itself would be
// wrong by exactly the output on every row.
func TestReadOccupancyAgreesWithOmpsOwnPerTurnTotal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		record      string
		totalTokens int64
		output      int64
	}{
		{name: "C line 247", record: turnC247, totalTokens: 131_300, output: 431},
		{name: "C line 250", record: turnC250, totalTokens: 134_693, output: 2_595},
		{name: "C line 254", record: turnC254, totalTokens: 40_748, output: 38},
		{name: "C line 271", record: turnC271, totalTokens: 45_483, output: 931},
		{name: "A line 8", record: turnA8, totalTokens: 49_003, output: 75},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := readOccupancy(writeTranscript(t, titleC1, sessionC2, tt.record))
			if err != nil {
				t.Fatalf("readOccupancy() error = %v", err)
			}
			if want := tt.totalTokens - tt.output; got.Occupancy != want {
				t.Fatalf("readOccupancy() = %d, want %d; %d is the per-turn total, which overstates by that turn's output", got.Occupancy, want, tt.totalTokens)
			}
		})
	}
}

// nonMessageTokens is the field whose name is the whole trap, and A line 8 is
// why: on that record it is 50213 while the prompt size is 48928, so it is not
// inside the window it describes. Four rows across A and B sit that way and 174
// sit the other, so neither adding nor subtracting it can be right. Naming both
// results here means a reader who reaches for it fails this test.
func TestReadOccupancyNeverReadsTheNonMessageCount(t *testing.T) {
	t.Parallel()

	const (
		promptSize         = 48_928 // A line 8's input + cacheRead + cacheWrite
		nonMessageTokens   = 50_213 // the same record's contextSnapshot.nonMessageTokens
		ifItWereAdded      = promptSize + nonMessageTokens
		ifItWereSubtracted = promptSize - nonMessageTokens // negative, which is the point
	)

	got, err := readOccupancy(writeTranscript(t, titleC1, sessionC2, turnA8))
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
	}
	if got.Occupancy != promptSize {
		t.Fatalf("readOccupancy() = %d, want %d; %d would be the count added and %d subtracted",
			got.Occupancy, promptSize, ifItWereAdded, ifItWereSubtracted)
	}
}

// A file caught mid-append: C lines 1, 2, 250 and the first 48 bytes of line
// 254, with no closing newline. The truncation is a stand-in — no real
// transcript contains a half-written record followed by whole ones. What it pins
// is only the unparseable-line skip, since the partial line here is the last
// one; the bounded tail's separate first-line drop belongs to
// internal/provider/transcript, which has its own case for it.
func TestReadOccupancyTruncatedFinalLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "truncated.jsonl")
	content := titleC1 + "\n" + sessionC2 + "\n" + turnC250 + "\n" + turnC254[:48]
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readOccupancy(path)
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
	}
	if got.Occupancy != 132_098 {
		t.Fatalf("readOccupancy() = %d, want 132098", got.Occupancy)
	}
}

// A transcript larger than the scan window, which is the case the bound exists
// for. The two ends are asserted together because they are what the reader's two
// reads separate: the occupancy comes from C line 254 at the very end, inside
// the window, while the version still comes from C line 2, megabytes behind it
// and outside the window entirely.
//
// The filler is usage-free toolResult records standing in for the run of large
// tool results a long agent session leaves behind. The size is realistic rather
// than absurd — the largest single record measured across the five transcripts
// is 60,190 bytes, also a toolResult — so a real omp transcript reaches this
// size in the low hundreds of records.
func TestReadOccupancyBeyondTheBoundedTail(t *testing.T) {
	t.Parallel()

	filler := `{"type":"message","id":"redacted","message":{"role":"toolResult","content":"` + strings.Repeat("x", 60_000) + `"}}`
	records := []string{titleC1, sessionC2, turnC250}
	for range int(maxScanBytes/int64(len(filler))) + 2 {
		records = append(records, filler)
	}
	path := writeTranscript(t, append(records, turnC254)...)

	// The premise, asserted rather than assumed: if the window ever grows past
	// the fixture, this test would keep passing while covering nothing.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= maxScanBytes {
		t.Fatalf("fixture is %d bytes, want more than the %d byte scan window", info.Size(), maxScanBytes)
	}

	got, err := readOccupancy(path)
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
	}
	if got.Occupancy != 40_710 {
		t.Fatalf("readOccupancy() = %d, want 40710", got.Occupancy)
	}
	if !sameVersion(got.TranscriptVersion, intPtr(3)) {
		t.Fatalf("readOccupancy() version = %s, want 3 read from beyond the window", formatVersion(got.TranscriptVersion))
	}
}

// A resolved transcript that is not there is this provider's evidence that the
// agent session is gone, and Superseded matches it by unwrapping to
// fs.ErrNotExist, so the wrapping has to keep that reachable.
func TestReadOccupancyMissingTranscript(t *testing.T) {
	t.Parallel()

	_, err := readOccupancy(filepath.Join(t.TempDir(), "missing.jsonl"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readOccupancy() error = %v, want it to unwrap to fs.ErrNotExist", err)
	}
}

func writeTranscript(t *testing.T, records ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(records, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func intPtr(value int) *int {
	return &value
}

func sameVersion(got, want *int) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func formatVersion(version *int) string {
	if version == nil {
		return "<nil>"
	}
	return strconv.Itoa(*version)
}
