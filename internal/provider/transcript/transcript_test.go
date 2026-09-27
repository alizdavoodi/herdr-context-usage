package transcript

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The records below are real first lines, kept because what this package
// promises is about position rather than content: which end of the file a
// record came from, and whether the bound cut it in half.
//
//   - piSession is line 1 of the pi transcript
//     ~/.pi/agent/sessions/--Users-alirezadavoodi-projects-github.com-alizdavoodi-herdr-context-usage--/2026-08-18T12-34-48-971Z_01a014de-020a-70b2-bc8e-b6a70ed3e784.jsonl,
//     measured at 195 bytes against pi 0.84.2.
//   - ompTitle and ompSession are lines 1 and 2 of the omp transcript
//     ~/.omp/agent/sessions/-projects-github.com-alizdavoodi-herdr-context-usage/2026-08-18T10-57-53-412Z_01a01485-4504-7000-ae6b-ace797eca8f5.jsonl,
//     measured against omp 17.3.5. ompTitle's pad field is abbreviated from the
//     real 128 spaces; it is the reason omp's session record is not line 1, and
//     the reason Head is a slice of records rather than one line.
const (
	piSession   = `{"type":"session","version":3,"id":"01a014de-020a-70b2-bc8e-b6a70ed3e784","timestamp":"2026-08-18T12:34:48.971Z","cwd":"/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"}`
	ompTitle    = `{"type":"title","v":1,"title":"Review q2, q5, q6, q7 feedback","source":"auto","updatedAt":"2026-08-18T11:13:14.011Z","pad":"    "}`
	ompSession  = `{"type":"session","version":3,"id":"01a01485-4504-7000-ae6b-ace797eca8f5","timestamp":"2026-08-18T10:57:53.412Z","cwd":"/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"}`
	ompNewest   = `{"type":"message","id":"8a99ff86","timestamp":"2026-08-18T11:15:01.800Z","message":{"role":"assistant","usage":{"input":132098,"output":2595,"cacheRead":0,"cacheWrite":0,"totalTokens":134693}}}`
	ompPostTurn = `{"type":"message","id":"32a5030c","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","usage":{"input":40710,"output":38,"cacheRead":0,"cacheWrite":0,"totalTokens":40748}}}`
)

// A transcript that fits inside the tail bound is the ordinary case: both ends
// are whole and the head is a prefix of the tail. Reading omp's real first two
// lines here is what pins that a caller looking for a session record has more
// than the first line to look at.
func TestReadReturnsWholeRecordsFromBothEnds(t *testing.T) {
	t.Parallel()

	path := write(t, ompTitle, ompSession, ompNewest, ompPostTurn)

	got, err := Read("omp", path, 8<<20)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(got.Head) < 2 || string(got.Head[0]) != ompTitle || string(got.Head[1]) != ompSession {
		t.Fatalf("Head() = %q, want omp's title record then its session record", got.Head)
	}
	if last := lastNonEmpty(got.Tail); last != ompPostTurn {
		t.Fatalf("Tail() ends with %q, want the last record in the file", last)
	}
}

// A transcript larger than the tail bound is the case the bound exists for, and
// the only one where the partial-record drops are reachable. Both are asserted
// together because they are opposite ends of the same read: the head comes from
// byte 0, megabytes behind a tail that begins inside a record.
func TestReadDropsTheRecordTheTailBoundOpenedInside(t *testing.T) {
	t.Parallel()

	const tailBytes int64 = 64 << 10
	// A run of large records standing in for the tool results a long agent
	// session leaves behind. The largest single record measured across the five
	// real omp transcripts is 60,190 bytes, so this size is realistic rather
	// than absurd.
	filler := `{"type":"message","message":{"role":"toolResult","content":"` + strings.Repeat("x", 20_000) + `"}}`
	records := []string{ompTitle, ompSession}
	for range int(tailBytes/int64(len(filler))) + 2 {
		records = append(records, filler)
	}
	path := write(t, append(records, ompPostTurn)...)

	got, err := Read("omp", path, tailBytes)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	// The premise, asserted rather than assumed: a fixture that stopped fitting
	// inside the bound would leave this test passing while covering nothing.
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Size() <= tailBytes {
		t.Fatalf("fixture is %d bytes, want more than the %d byte bound", info.Size(), tailBytes)
	}
	if string(got.Head[1]) != ompSession {
		t.Fatalf("Head()[1] = %q, want the session record read from beyond the tail bound", got.Head[1])
	}
	for _, line := range got.Tail {
		if len(line) > 0 && line[0] != '{' {
			t.Fatalf("Tail() contains %q, which does not begin a record; the partial first line was not dropped", truncate(line))
		}
	}
	if last := lastNonEmpty(got.Tail); last != ompPostTurn {
		t.Fatalf("Tail() ends with %q, want the last record in the file", last)
	}
}

// The head bound has its own cut to make, and it is not the tail's: a first
// record longer than the bound leaves no whole record in the head at all. No
// real transcript has one — pi's session record is 195 bytes and omp's title
// record is a few hundred — so this stands in for a store that grew one, where
// the answer must be no version rather than half a record handed to a decoder.
func TestReadDropsTheRecordTheHeadBoundCutInHalf(t *testing.T) {
	t.Parallel()

	oversized := `{"type":"title","pad":"` + strings.Repeat(" ", int(headBytes)) + `"}`
	path := write(t, oversized, ompSession, ompPostTurn)

	got, err := Read("omp", path, 8<<20)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	for _, line := range got.Head {
		if int64(len(line)) >= headBytes {
			t.Fatalf("Head() contains a %d byte line, want the record the bound cut in half dropped", len(line))
		}
	}
	// The tail is unaffected: it is read from the other end and the oversized
	// record is nowhere near it.
	if last := lastNonEmpty(got.Tail); last != ompPostTurn {
		t.Fatalf("Tail() ends with %q, want the last record in the file", last)
	}
}

// An empty transcript is what an agent session leaves behind between creating
// the file and writing its first record. It is not an error, and every caller
// walks the result rather than indexing it, so no answer is the right answer.
func TestReadAnEmptyTranscript(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Read("pi", path, 8<<20)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if lastNonEmpty(got.Tail) != "" || lastNonEmpty(got.Head) != "" {
		t.Fatalf("Read() = %#v, want no records from an empty file", got)
	}
}

// Each provider's Superseded matches a missing transcript by unwrapping
// to fs.ErrNotExist, so the wrapping here has to keep that reachable, and each
// provider's own semantic test constructs the message this produces. A shared
// message with no agent in it would leave a maintenance diagnostic unable to
// say which store failed.
func TestReadNamesTheAgentAndPreservesTheOpenError(t *testing.T) {
	t.Parallel()

	for _, agent := range []string{"claude", "pi", "omp"} {
		_, err := Read(agent, filepath.Join(t.TempDir(), "missing.jsonl"), 8<<20)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("Read(%q) error = %v, want it to unwrap to fs.ErrNotExist", agent, err)
		}
		if want := "open " + agent + " transcript: "; !strings.HasPrefix(err.Error(), want) {
			t.Fatalf("Read(%q) error = %q, want it to begin %q", agent, err, want)
		}
	}
}

// A directory opens and does not read, which is the same class of answer as the
// permission error the transient classification exists for. It reaches the
// caller as an error rather than as an empty scan, because an empty scan would
// be read as an agent session with no completed turn and blank the row.
func TestReadADirectoryFails(t *testing.T) {
	t.Parallel()

	if _, err := Read("omp", t.TempDir(), 8<<20); err == nil {
		t.Fatal("Read() error = nil, want a directory to fail rather than scan empty")
	}
}

func write(t *testing.T, records ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(records, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// lastNonEmpty is what every caller's backwards walk does with the blank line a
// trailing newline leaves behind, so the assertions use it rather than pinning
// where that blank lands.
func lastNonEmpty(lines [][]byte) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) > 0 {
			return string(lines[i])
		}
	}
	return ""
}

func truncate(line []byte) string {
	const limit = 60
	if len(line) <= limit {
		return string(line)
	}
	return string(line[:limit]) + "..."
}

// One documented gap. Read returns a head it managed to read alongside a tail
// read that failed, and pi's reader depends on that to record the schema version
// on a blank row. No portable fixture produces that state: on a regular file the
// seek does not fail and the tail read fails only on failing hardware, and the
// one target that does fail — a directory — fails the head read first, so its
// head is empty for a different reason. The behaviour is therefore stated in
// Read's documentation rather than claimed as covered here.
