package pi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/piompshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/transcript"
)

// pi writes whole tool results into the transcript — the largest single record
// observed across the two real transcripts is 99,827 bytes, a toolResult — so
// the bound has to hold a run of those and still reach the assistant record
// underneath. 8 MiB leaves room for roughly eighty of them while keeping the
// scan independent of how long the agent session has run.
const maxScanBytes int64 = 8 << 20

var (
	ErrNoOccupancy   = errors.New("pi transcript has no current context occupancy")
	ErrJustCompacted = fmt.Errorf("pi %w", provider.ErrJustCompacted)
)

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

func readOccupancy(path string) (provider.Reading, error) {
	scan, err := transcript.Read("pi", path, maxScanBytes)
	// The version rides on the failure path too. A tail read that fails after
	// the head was read still knows the format it was reading, and that is the
	// case where knowing it matters most: a blank row caused by a schema change
	// is otherwise indistinguishable from any other unreadable transcript.
	version := readVersion(scan.Head)
	if err != nil {
		return provider.Reading{TranscriptVersion: version}, err
	}
	return newestOccupancy(scan.Tail, version)
}

// newestOccupancy walks back from the end of the transcript to the newest
// completed assistant turn. version rides on every return, including the two
// errors, so that a blank row still records the format it was read at.
func newestOccupancy(lines [][]byte, version *int) (provider.Reading, error) {
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
			return provider.Reading{TranscriptVersion: version}, ErrJustCompacted
		}
		if entry.Message == nil || entry.Message.Role != "assistant" || entry.Message.Usage == nil {
			// The gate is the role plus a usage, never stopReason: both observed
			// stop reasons carry a usage — toolUse on 10 records and stop on 3 in
			// the transcript measured — and the newest of either is the current
			// window. user and toolResult records carry no usage at all.
			continue
		}
		// Which three counts these are is pi's business — the shapes around them
		// diverge from omp's even where the field names do not — while the sum
		// itself is the only thing ADR-0006 found measured identical on both
		// agents, so it is the only thing that lives in a shared package.
		usage := entry.Message.Usage
		if occupancy, ok := piompshared.Occupancy(usage.Input, usage.CacheRead, usage.CacheWrite); ok {
			return provider.Reading{Occupancy: occupancy, TranscriptVersion: version}, nil
		}
	}
	return provider.Reading{TranscriptVersion: version}, ErrNoOccupancy
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
// the head read shares its open with the tail read, which meets the same failure
// and wraps it, where it classifies as transient and preserves the displayed
// value.
//
// Only the first line is considered, and that is pi-specific rather than
// incidental: pi writes the session record first, where omp writes a fixed-width
// title record ahead of it. Scanning further here would accept a shape pi does
// not produce.
func readVersion(head [][]byte) *int {
	if len(head) == 0 {
		return nil
	}
	var record sessionRecord
	if json.Unmarshal(bytes.TrimSpace(head[0]), &record) != nil || record.Type != "session" {
		return nil
	}
	return record.Version
}
