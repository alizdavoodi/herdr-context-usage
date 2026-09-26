package pi

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Provenance for every fixture in this file, measured against pi 0.84.2 and
// herdr 0.8.0. Two real transcripts exist on this machine and both are used:
//
//	A = ~/.pi/agent/sessions/--Users-alirezadavoodi-projects-github.com-alizdavoodi-herdr-context-usage--/2026-08-18T12-34-48-971Z_01a014de-020a-70b2-bc8e-b6a70ed3e784.jsonl
//	    42 lines, the only compacted pi agent session available. Line 1 is the
//	    session record. Line 37 and line 39 are consecutive assistant turns, with
//	    a toolResult at line 38 between them. Line 39 is the newest completed
//	    turn before the compaction, which is line 40 and was the last record in
//	    the file when this provider was specified. Line 41 is the user record
//	    that reopened the agent session and line 42 is the first post-compaction
//	    assistant turn.
//	B = ~/.pi/agent/sessions/--Users-alirezadavoodi--/2026-08-16T20-45-15-107Z_01a00c52-4be3-7232-a529-2c4322257dd6.jsonl
//	    79 lines, 22 usage rows, no compaction. Only its line 68, the last usage
//	    row, is used here.
//
// Every usage object is copied exactly as written. The surrounding message
// fields are reduced to role, stopReason and the record timestamp, and ids are
// kept only where they were read: content, models, responseIds and cost objects
// carry nothing this reader decodes.
//
// Position is what these fixtures are for. A record of the right shape at the
// wrong position measures the wrong instant, which is what both earlier codex
// regressions in this repository were, so each case below says where in A or B
// its records came from and in what order they are being replayed.
const (
	// A line 1, as written.
	sessionV3 = `{"type":"session","version":3,"id":"01a014de-020a-70b2-bc8e-b6a70ed3e784","timestamp":"2026-08-18T12:34:48.971Z","cwd":"/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"}`

	// A stand-in: both real session records stamp version 3, as does omp's, so
	// no other value has been observed anywhere. That is the point of the case
	// it serves — the version must not gate the measurement.
	sessionV4 = `{"type":"session","version":4,"id":"01a014de-020a-70b2-bc8e-b6a70ed3e784","timestamp":"2026-08-18T12:34:48.971Z","cwd":"/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"}`

	// A line 37, an assistant turn that stopped to call a tool: 578 + 25216 + 0
	// = 25794. It exists so the newest turn has something older to beat.
	turnA37 = `{"id":"redacted","type":"message","parentId":"redacted","timestamp":"2026-08-18T12:37:08.871Z","message":{"role":"assistant","stopReason":"toolUse","usage":{"input":578,"output":1466,"cacheRead":25216,"cacheWrite":0,"reasoning":1355,"totalTokens":27260}}}`

	// A lines 2 and 3, the bookkeeping records pi writes when the model or the
	// thinking level changes. They carry no message field at all, and they are
	// not rare: B holds 27 thinking_level_change records among its 79 lines.
	modelChangeA2 = `{"type":"model_change","id":"c9050d3e","parentId":null,"timestamp":"2026-08-18T12:34:49.091Z","provider":"amazon-bedrock","modelId":"us.anthropic.claude-opus-4-6-v1"}`

	// A line 2 with a version key added. A stand-in with no precedent: only the
	// session record carries a version in either transcript. It exists because
	// without it the type check in readVersion cannot be shown to do anything —
	// every other non-session fixture lacks a version, so the version comes back
	// nil whether the check is there or not, and the case that reads as though it
	// covers the check passes with the check deleted.
	versionedNonSessionRecord = `{"type":"model_change","version":9,"id":"c9050d3e","parentId":null,"timestamp":"2026-08-18T12:34:49.091Z","provider":"amazon-bedrock","modelId":"us.anthropic.claude-opus-4-6-v1"}`

	thinkingLevelChangeA3 = `{"type":"thinking_level_change","id":"8d7c847f","parentId":"c9050d3e","timestamp":"2026-08-18T12:34:49.091Z","thinkingLevel":"high"}`

	// A line 38, the toolResult between the two turns. It carries no usage at
	// all, which is true of every user and toolResult record in both transcripts.
	toolResultA38 = `{"id":"redacted","type":"message","parentId":"redacted","timestamp":"2026-08-18T12:37:08.879Z","message":{"content":[],"isError":false,"role":"toolResult","toolCallId":"redacted","toolName":"redacted"}}`

	// A line 39, the newest completed turn before A's compaction: 769 + 27136 +
	// 0 = 27905. Its totalTokens of 30458 is that sum plus the turn's output of
	// 2553, and its reasoning of 1394 is a subset of that output — the two
	// numbers that must never be read are both present in the fixture on
	// purpose.
	turnA39 = `{"id":"00955bc3","type":"message","parentId":"fccb0027","timestamp":"2026-08-18T12:37:34.783Z","message":{"role":"assistant","stopReason":"stop","usage":{"input":769,"output":2553,"cacheRead":27136,"cacheWrite":0,"reasoning":1394,"totalTokens":30458}}}`

	// A line 40, the compaction record, with its own usage intact because that
	// usage is the trap: 1383 + 0 + 0 = 1383 is plausible for a freshly
	// compacted agent session. tokensBefore is kept for the same reason — it is
	// 30458, line 39's per-turn total, describing the agent session before the
	// compaction. The summary is abbreviated; nothing else is changed.
	compactionA40 = `{"id":"redacted","type":"compaction","parentId":"redacted","timestamp":"2026-08-18T12:50:17.206Z","tokensBefore":30458,"usage":{"input":1383,"output":602,"cacheRead":0,"cacheWrite":0,"reasoning":158,"totalTokens":1985},"details":{"readFiles":[],"modifiedFiles":[]},"firstKeptEntryId":"67d66b63","fromHook":false,"summary":"No prior history."}`

	// A line 41, the user record that reopened the agent session after the
	// compaction.
	userA41 = `{"id":"512774df","type":"message","parentId":"ace9e4fe","timestamp":"2026-08-18T14:48:07.648Z","message":{"content":[],"role":"user"}}`

	// A line 42, the first assistant turn after A's compaction, and the
	// measurement that closes this ticket's stated precondition: 17033 + 0 + 0 =
	// 17033, against 27905 immediately before the compaction. The identity holds
	// here too, 17033 + 18 = 17051 = totalTokens, so no compensating arithmetic
	// is needed once the walk stops at line 40. Note that pi does not zero the
	// breakdown the way codex does: input carries the whole post-compaction
	// prefix and cacheRead is 0 only because that prefix has not been cached
	// yet.
	turnA42 = `{"id":"37a1085f","type":"message","parentId":"redacted","timestamp":"2026-08-18T14:48:10.047Z","message":{"role":"assistant","stopReason":"stop","usage":{"input":17033,"output":18,"cacheRead":0,"cacheWrite":0,"reasoning":16,"totalTokens":17051}}}`

	// A line 42 with the two cache keys deleted. A stand-in: all 35 usage rows
	// across A and B write all four counts, so an absent key has no precedent.
	// The answer is unchanged at 17033, which is what makes it worth pinning —
	// an absent count must read as zero rather than as an unusable row.
	turnA42NoCacheKeys = `{"id":"37a1085f","type":"message","parentId":"redacted","timestamp":"2026-08-18T14:48:10.047Z","message":{"role":"assistant","stopReason":"stop","usage":{"input":17033,"output":18,"reasoning":16,"totalTokens":17051}}}`

	// A line 42 with every count zeroed. A stand-in: no usage row in either
	// transcript is all-zero. It stands in for a turn that failed before it
	// consumed anything.
	turnA42AllZero = `{"id":"37a1085f","type":"message","parentId":"redacted","timestamp":"2026-08-18T14:48:10.047Z","message":{"role":"assistant","stopReason":"stop","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":0}}}`

	// A line 42 with cacheRead negated. A stand-in with no precedent anywhere:
	// no count in either transcript is negative, and pi has no reason to write
	// one. The guard against it is therefore defensive, which is exactly why it
	// needs a case — an untested guard can be deleted without anything noticing,
	// and its deletion would turn a corrupt row into a plausible small number
	// rather than into no answer at all.
	turnA42NegativeCache = `{"id":"37a1085f","type":"message","parentId":"redacted","timestamp":"2026-08-18T14:48:10.047Z","message":{"role":"assistant","stopReason":"stop","usage":{"input":17033,"output":18,"cacheRead":-16000,"cacheWrite":0,"reasoning":16,"totalTokens":17051}}}`

	// A line 42 with its usage object removed, standing in for an assistant
	// record whose turn has not completed. No such record exists in either
	// transcript — every one of the 13 assistant records in A carries a usage —
	// because pi writes the record once the turn is done.
	turnA42NoUsage = `{"id":"37a1085f","type":"message","parentId":"redacted","timestamp":"2026-08-18T14:48:10.047Z","message":{"role":"assistant","stopReason":"stop"}}`

	// B line 68, B's last usage row, with cacheWrite raised from 0 to 3500 and
	// totalTokens raised from 41602 to 45102 so the fixture still satisfies pi's
	// measured identity. A stand-in for that one field: cacheWrite is 0 on all
	// 35 real usage rows, so a turn writing cache has never been captured here.
	// The expected 796 + 39680 + 3500 = 43976 is reachable from no other
	// combination in the record, least of all from the 45102 total.
	turnB68CacheWrite = `{"id":"redacted","type":"message","parentId":"redacted","timestamp":"2026-08-16T21:02:13.000Z","message":{"role":"assistant","stopReason":"stop","usage":{"input":796,"output":1126,"cacheRead":39680,"cacheWrite":3500,"reasoning":0,"totalTokens":45102}}}`

	// A splice with no real precedent: A line 38's toolResult carrying A line
	// 39's usage. Only assistant records carry a usage in either transcript, so
	// this exists purely to keep the role half of the gate load-bearing — without
	// it, gating on the presence of a usage alone would pass every other case
	// here, and a usage appearing on some other role later would silently be read
	// as a completed turn.
	toolResultWithUsage = `{"id":"redacted","type":"message","parentId":"redacted","timestamp":"2026-08-18T12:37:08.879Z","message":{"content":[],"isError":false,"role":"toolResult","toolCallId":"redacted","toolName":"redacted","usage":{"input":769,"output":2553,"cacheRead":27136,"cacheWrite":0,"reasoning":1394,"totalTokens":30458}}}`

	// Not from either transcript: a line that cannot be parsed at all.
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
			// A lines 1, 37, 38 and 39 in their real order. The newest wins, and
			// its cache count is an addend: 769 + 27136 + 0, not the 769 that
			// reading input alone would report and not the 30458 its own total
			// carries.
			name:        "sums the newest completed assistant turn",
			records:     []string{sessionV3, turnA37, toolResultA38, turnA39},
			want:        27_905,
			wantVersion: intPtr(3),
		},
		{
			name:        "an absent cache count reads as zero",
			records:     []string{sessionV3, turnA42NoCacheKeys},
			want:        17_033,
			wantVersion: intPtr(3),
		},
		{
			name:        "adds input and both cache counts",
			records:     []string{sessionV3, turnB68CacheWrite},
			want:        43_976,
			wantVersion: intPtr(3),
		},
		{
			// A lines 1, 39 and 40: the file exactly as it stood between the
			// compaction and the next turn. Walking past line 40 would report
			// 27905, the size of the context that had just been thrown away.
			name:        "stops at a compaction record and reports the version",
			records:     []string{sessionV3, turnA39, compactionA40},
			wantVersion: intPtr(3),
			wantErr:     ErrJustCompacted,
		},
		{
			// A lines 1, 39, 40, 41 and 42 in their real order. The walk stops at
			// the newest compaction, so once line 42 exists it is the answer, and
			// the pre-compaction 27905 behind line 40 is unreachable without any
			// compensating arithmetic.
			name:        "measures the completed turn after a compaction",
			records:     []string{sessionV3, turnA39, compactionA40, userA41, turnA42},
			want:        17_033,
			wantVersion: intPtr(3),
		},
		{
			name:        "skips an assistant record whose turn has not completed",
			records:     []string{sessionV3, turnA39, turnA42NoUsage},
			want:        27_905,
			wantVersion: intPtr(3),
		},
		{
			name:        "skips an all-zero usage row rather than reporting zero",
			records:     []string{sessionV3, turnA39, turnA42AllZero},
			want:        27_905,
			wantVersion: intPtr(3),
		},
		{
			// Without the guard this row sums to 1,033 and is reported as the
			// current occupancy — small, plausible, and wrong. Skipping it
			// reaches the previous completed turn instead.
			name:        "skips a row carrying a negative count",
			records:     []string{sessionV3, turnA39, turnA42NegativeCache},
			want:        27_905,
			wantVersion: intPtr(3),
		},
		{
			name:        "an unparseable line does not mask an earlier turn",
			records:     []string{sessionV3, turnA39, malformedLine},
			want:        27_905,
			wantVersion: intPtr(3),
		},
		{
			// A lines 38 and 41 replayed after line 39, which is not their real
			// order: what is being pinned is that neither role ends the walk.
			name:        "skips user and toolResult records",
			records:     []string{sessionV3, turnA39, toolResultA38, userA41},
			want:        27_905,
			wantVersion: intPtr(3),
		},
		{
			// A lines 2 and 3 replayed after line 39, which is not where they
			// occur: what is being pinned is that a record carrying no message
			// field at all does not end the walk.
			name:        "skips bookkeeping records that carry no message",
			records:     []string{sessionV3, turnA39, modelChangeA2, thinkingLevelChangeA3},
			want:        27_905,
			wantVersion: intPtr(3),
		},
		{
			// A line 37 followed by the spliced toolResult carrying line 39's
			// usage. The answer is line 37's 25794; 27905 would mean the walk had
			// accepted a usage on a role that is not an assistant turn.
			name:        "a usage on a role that is not an assistant turn is not a turn",
			records:     []string{sessionV3, turnA37, toolResultWithUsage},
			want:        25_794,
			wantVersion: intPtr(3),
		},
		{
			// A lines 1, 41 and 38 with no assistant record anywhere, which is what
			// an agent session that has not completed a turn looks like. Unknown is
			// an error rather than a zero.
			name:        "a transcript with no assistant turn is unknown",
			records:     []string{sessionV3, userA41, toolResultA38},
			wantVersion: intPtr(3),
			wantErr:     ErrNoOccupancy,
		},
		{
			name:        "measures at an unexpected schema version and reports it",
			records:     []string{sessionV4, turnA39},
			want:        27_905,
			wantVersion: intPtr(4),
		},
		{
			name:    "a missing session record leaves the version unknown",
			records: []string{turnA39},
			want:    27_905,
		},
		{
			name:    "an unparseable first line leaves the version unknown",
			records: []string{malformedLine, turnA39},
			want:    27_905,
		},
		{
			name:    "a first line that is not a session record leaves the version unknown",
			records: []string{toolResultA38, turnA39},
			want:    27_905,
		},
		{
			// The case above cannot fail on its own: its first line carries no
			// version, so the version is nil whether or not the type is checked.
			// This one carries a version on a record that is not the session
			// record, which is the only shape that tells the two apart.
			name:    "a version on a record that is not the session record is not read",
			records: []string{versionedNonSessionRecord, turnA39},
			want:    27_905,
		},
		{
			// A stand-in for a shape pi does not write: both real transcripts
			// put the session record at line 1, and this puts A line 2 there
			// with the session record behind it. That is omp's header shape — a
			// record ahead of the session record — replayed into a pi
			// transcript, and it is the state that pins the search to the first
			// line rather than to the first session record it can find. A
			// reader that walked the head the way omp's has to would report
			// version 3 here, for a transcript pi never produced.
			name:    "a session record behind the first line is not read",
			records: []string{modelChangeA2, sessionV3, turnA39},
			want:    27_905,
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

// The whole point of the pi provider. pi's compaction record carries a usage of
// its own, the cost of generating the summary, so a walk that stopped at the
// newest record holding a usage would read A line 40 and report 1383 — plausible
// for a freshly compacted agent session and therefore the kind of wrong number
// nobody questions. 1383 is named here so that such a change fails this test
// rather than shipping.
func TestReadOccupancyNeverReadsTheCompactionRecordsOwnUsage(t *testing.T) {
	t.Parallel()

	const compactionRecordUsageSum = 1_383 // 1383 + 0 + 0, A line 40's own usage

	got, err := readOccupancy(writeTranscript(t, sessionV3, turnA39, compactionA40))
	if !errors.Is(err, ErrJustCompacted) {
		t.Fatalf("readOccupancy() error = %v, want %v", err, ErrJustCompacted)
	}
	if got.Occupancy == compactionRecordUsageSum {
		t.Fatalf("readOccupancy() = %d, the compaction record's own usage; the walk must stop on the record type, not on the presence of a usage", got.Occupancy)
	}
	if got.Occupancy != 0 {
		t.Fatalf("readOccupancy() = %d, want no occupancy at a compaction boundary", got.Occupancy)
	}
}

// pi's own per-turn total is the oracle for the arithmetic: on all 35 usage rows
// across the two transcripts, input + cacheRead + cacheWrite + output equals
// totalTokens, which leaves no room for a cache count to be a subset. So the
// occupancy has to come out at totalTokens minus that turn's output, and a
// reader that reported the total itself would be wrong by exactly the output on
// every row.
func TestReadOccupancyAgreesWithPisOwnPerTurnTotal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		record      string
		totalTokens int64
		output      int64
	}{
		{name: "A line 37", record: turnA37, totalTokens: 27_260, output: 1_466},
		{name: "A line 39", record: turnA39, totalTokens: 30_458, output: 2_553},
		{name: "A line 42", record: turnA42, totalTokens: 17_051, output: 18},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := readOccupancy(writeTranscript(t, sessionV3, tt.record))
			if err != nil {
				t.Fatalf("readOccupancy() error = %v", err)
			}
			if want := tt.totalTokens - tt.output; got.Occupancy != want {
				t.Fatalf("readOccupancy() = %d, want %d; %d is the per-turn total, which overstates by that turn's output", got.Occupancy, want, tt.totalTokens)
			}
		})
	}
}

// A file caught mid-append: A lines 1, 39 and the first 48 bytes of line 42,
// with no closing newline. The truncation is a stand-in — no real transcript
// contains a half-written record followed by whole ones. What it pins is only
// the unparseable-line skip, since the partial line here is the last one; the
// bounded tail's separate first-line drop is reached only by a transcript larger
// than the scan window, which TestReadOccupancyBeyondTheBoundedTail builds.
func TestReadOccupancyTruncatedFinalLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "truncated.jsonl")
	content := sessionV3 + "\n" + turnA39 + "\n" + turnA42[:48]
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readOccupancy(path)
	if err != nil {
		t.Fatalf("readOccupancy() error = %v", err)
	}
	if got.Occupancy != 27_905 {
		t.Fatalf("readOccupancy() = %d, want 27905", got.Occupancy)
	}
}

// A transcript larger than the scan window, which is the only way to reach the
// bounded tail's first-line drop: the scan opens partway into a record, and
// those leading bytes are discarded because they are not a whole one.
//
// The filler is usage-free toolResult records standing in for the run of large
// tool results a long agent session leaves behind. The size is realistic rather
// than absurd — the largest single record measured across the two transcripts is
// 99,827 bytes, also a toolResult — so a real pi transcript reaches this size in
// the low hundreds of records.
//
// Two things are asserted together because they are what the reader's two reads
// separate: the occupancy comes from A line 42 at the very end, inside the
// window, while the version still comes from A line 1, megabytes behind it and
// outside the window entirely.
func TestReadOccupancyBeyondTheBoundedTail(t *testing.T) {
	t.Parallel()

	filler := `{"id":"redacted","type":"message","parentId":"redacted","message":{"role":"toolResult","content":"` + strings.Repeat("x", 100_000) + `"}}`
	records := []string{sessionV3, turnA39}
	for range int(maxScanBytes/int64(len(filler))) + 2 {
		records = append(records, filler)
	}
	path := writeTranscript(t, append(records, turnA42)...)

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
	if got.Occupancy != 17_033 {
		t.Fatalf("readOccupancy() = %d, want 17033", got.Occupancy)
	}
	if !sameVersion(got.TranscriptVersion, intPtr(3)) {
		t.Fatalf("readOccupancy() version = %s, want 3 read from beyond the window", formatVersion(got.TranscriptVersion))
	}
}

// A resolved transcript that is not there is this provider's evidence that the
// agent session is gone, and States classifies it by unwrapping to
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
