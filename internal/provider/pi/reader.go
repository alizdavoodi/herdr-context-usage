package pi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// pi writes whole tool results into the transcript — the largest single record
// observed across the two real transcripts is 99,827 bytes, a toolResult — so
// the bound has to hold a run of those and still reach the assistant record
// underneath. 8 MiB leaves room for roughly eighty of them while keeping the
// scan independent of how long the agent session has run.
const maxScanBytes int64 = 8 << 20

// The session record is the transcript's first line and measured 195 bytes, 196
// counting its newline. This bound exists only so that a first line this reader
// cannot understand still cannot be read unboundedly.
const maxHeaderBytes int64 = 64 << 10

var (
	ErrNoOccupancy   = errors.New("pi transcript has no current context occupancy")
	ErrJustCompacted = errors.New("pi agent session is just-compacted")
)

// Reading is what the reader observed. Version is the schema version stamped on
// the transcript's session record, nil when absent. It is populated even when
// the error is non-nil, so a just-compacted row still records the version it was
// read at — the caller must therefore read Version on the error path too.
type Reading struct {
	Occupancy int64
	Version   *int
}

type sessionRecord struct {
	Type    string `json:"type"`
	Version *int   `json:"version"`
}

// transcriptEntry decodes only what the walk decides on, and the fields it
// leaves out are the point.
//
// There is no top-level usage field, so pi's compaction record — which carries
// one, being the cost of generating the summary — cannot contribute a number
// here however the walk is later edited. Nor a totalTokens field: pi's per-turn
// total is the occupancy plus that turn's output, 30,458 against the correct
// 27,905 on the newest completed turn measured. On codex the equivalently named
// field is the one to prefer, for a reason particular to codex, so neither
// provider's rule may be copied to the other. Nor a tokensBefore field: the
// compaction record's figure is that same per-turn total, 30,458, and it
// describes the agent session before the compaction, which is wrong twice over.
// Nor a reasoning field: 1,394 on that turn, a subset of its output of 2,553.
type transcriptEntry struct {
	Type    string `json:"type"`
	Message *struct {
		Role  string `json:"role"`
		Usage *struct {
			Input      int64 `json:"input"`
			CacheRead  int64 `json:"cacheRead"`
			CacheWrite int64 `json:"cacheWrite"`
		} `json:"usage"`
	} `json:"message"`
}

func ReadOccupancy(path string) (Reading, error) {
	file, err := os.Open(path)
	if err != nil {
		return Reading{}, fmt.Errorf("open pi transcript: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Reading{}, fmt.Errorf("stat pi transcript: %w", err)
	}

	version := readVersion(file)
	start := max(info.Size()-maxScanBytes, 0)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return Reading{Version: version}, fmt.Errorf("seek pi transcript: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxScanBytes))
	if err != nil {
		return Reading{Version: version}, fmt.Errorf("read pi transcript tail: %w", err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	if start > 0 && len(lines) > 0 {
		// The first bytes may be the tail of a record the scan opened inside.
		lines = lines[1:]
	}
	return newestOccupancy(lines, version)
}

// newestOccupancy walks back from the end of the transcript to the newest
// completed assistant turn. version rides on every return, including the two
// errors, so that a blank row still records the format it was read at.
func newestOccupancy(lines [][]byte, version *int) (Reading, error) {
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var entry transcriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// A truncated final line looks like this, as does the partial record
			// the bounded tail opens inside. Skipping keeps an earlier completed
			// turn reachable instead of ending the walk on it.
			continue
		}
		if entry.Type == "compaction" {
			// Stopping keys on the record type and never on the presence of a
			// usage. pi's compaction record carries its own, and it sums to 1,383
			// on the one compaction measured: plausible for a freshly compacted
			// agent session, and therefore the kind of wrong number nobody
			// questions. transcriptEntry decodes no top-level usage, which makes
			// reading it structurally impossible rather than merely forbidden.
			return Reading{Version: version}, ErrJustCompacted
		}
		if entry.Message == nil || entry.Message.Role != "assistant" || entry.Message.Usage == nil {
			// The gate is the role plus a usage, never stopReason: both observed
			// stop reasons carry a usage — toolUse on 10 records and stop on 3 in
			// the transcript measured — and the newest of either is the current
			// window. user and toolResult records carry no usage at all.
			continue
		}
		if occupancy, ok := occupancyOf(entry); ok {
			return Reading{Occupancy: occupancy, Version: version}, nil
		}
	}
	return Reading{Version: version}, ErrNoOccupancy
}

// occupancyOf sums the three counts describing the window the turn was sent
// with, and reports whether the row yields a number at all.
//
// The three are addends rather than subsets, and that is measured: pi's own
// per-turn total equals this sum plus the turn's output on all 35 usage rows of
// the two real transcripts, which leaves no room for a subset relation.
func occupancyOf(entry transcriptEntry) (int64, bool) {
	usage := entry.Message.Usage
	if usage.Input < 0 || usage.CacheRead < 0 || usage.CacheWrite < 0 {
		return 0, false
	}
	occupancy := usage.Input + usage.CacheRead + usage.CacheWrite
	if occupancy == 0 {
		// An all-zero usage row is a failed turn rather than an empty window, so
		// the walk continues to the previous reading; internal/provider/claude/reader.go
		// carries the evidence for that and for why unknown stays null, not zero.
		return 0, false
	}
	return occupancy, true
}

// readVersion reads the schema version off the transcript's session record.
//
// The version never gates a measurement. pi and omp both stamp version 3 while
// carrying different field sets, so it distinguishes neither the two agents nor
// a breaking change within one: gating on it would reject a harmless increment
// and accept a breaking change at the same number. It is carried into the
// reading instead, so a format change in a floating dependency shows up in the
// output rather than only as a blank row.
//
// An absent, unparseable or non-session first line therefore leaves the version
// nil and the walk proceeds. A read failure is not reported from here either:
// the tail read meets the same failure and wraps it, where it classifies as
// transient and preserves the displayed value.
func readVersion(r io.Reader) *int {
	line, err := bufio.NewReader(io.LimitReader(r, maxHeaderBytes)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	var record sessionRecord
	if json.Unmarshal(bytes.TrimSpace(line), &record) != nil || record.Type != "session" {
		return nil
	}
	return record.Version
}
