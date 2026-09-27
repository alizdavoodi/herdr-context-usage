// Package transcript reads the two ends of a JSONL transcript that a provider
// needs and nothing in between: the head, where the agents that stamp a schema
// version put it, and a bounded tail, where the newest completed turn is.
//
// claude, pi and omp share the open-stat-seek-read-split prologue, so the
// partial-line handling lives in one place.
//
// What may live here is only what decodes nothing. This package hands back
// bytes and never looks inside a record, which is what keeps it clear of
// ADR-0006: the directory-slug derivation, the per-turn usage shape, the
// compaction record shape and the model-change record shape all differ between
// the agents and each stays in its own provider. A function here that knew what
// a "compaction" record was would be the merge that ADR says will keep
// presenting itself as an obvious simplification.
package transcript

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// headBytes bounds the read that looks for a session record.
//
// The record is not always the first line. pi writes it first, measured at 195
// bytes; omp writes a fixed-width title record ahead of it, so its session
// record is line 2 on all five real omp transcripts. This bound therefore has
// to hold several records rather than one, and 64 KiB holds well over a hundred
// of either agent's header records. It exists so that a head this reader cannot
// understand still cannot be read unboundedly.
const headBytes int64 = 64 << 10

// Scan is a transcript's two ends, split into whole records. A record that the
// bound cut in half is dropped from both — the head loses a partial last line,
// the tail a partial first one — so a caller never decodes half a record and
// mistakes the failure for a malformed one.
type Scan struct {
	Head [][]byte
	Tail [][]byte
}

// Read opens path and returns its head and its last tailBytes.
//
// agent names the caller in every error, because these errors reach a
// maintenance diagnostic where "open transcript" with no agent in it would not
// say which store failed.
//
// The head read is unconditional even though claude has no version to find in
// it. Making it optional would buy one bounded read at a fixed offset and cost
// a parameter, a branch, and a way for a provider to silently lose its version
// by forgetting to ask. For a transcript below the tail bound the two ranges
// overlap, so its first 64 KiB is read twice — roughly a 1% overhead on the
// common case, paid to keep one code path.
//
// A head that was read survives a tail read that fails, so the Scan is
// meaningful alongside a non-nil error and callers must not discard it there.
// That is what lets a blank row still record the schema version it was read at:
// a format change in a floating dependency is otherwise indistinguishable from
// any other unreadable transcript.
func Read(agent, path string, tailBytes int64) (Scan, error) {
	file, err := os.Open(path)
	if err != nil {
		return Scan{}, fmt.Errorf("open %s transcript: %w", agent, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Scan{}, fmt.Errorf("stat %s transcript: %w", agent, err)
	}

	// A head that cannot be read is not evidence about the tail, so it is
	// swallowed rather than returned: the caller loses the version and reports
	// it as unknown. A failure that matters reappears on the tail read below,
	// where it is wrapped and classifies as transient, preserving the displayed
	// value.
	raw, _ := io.ReadAll(io.LimitReader(file, headBytes))
	scan := Scan{Head: wholeHead(raw)}

	start := max(info.Size()-tailBytes, 0)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return scan, fmt.Errorf("seek %s transcript: %w", agent, err)
	}
	tail, err := io.ReadAll(io.LimitReader(file, tailBytes))
	if err != nil {
		return scan, fmt.Errorf("read %s transcript tail: %w", agent, err)
	}

	scan.Tail = wholeTail(tail, start)
	return scan, nil
}

// wholeHead drops a trailing record the head bound cut in half. Reading exactly
// the bound is how a cut is detected: a shorter read reached the end of the file
// and every line in it is whole.
func wholeHead(head []byte) [][]byte {
	lines := bytes.Split(head, []byte{'\n'})
	if int64(len(head)) == headBytes && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// wholeTail drops the leading bytes of a record the scan opened inside. start
// is the test rather than the length, because a scan that began at the start of
// the file opened inside nothing.
func wholeTail(tail []byte, start int64) [][]byte {
	lines := bytes.Split(tail, []byte{'\n'})
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	return lines
}
