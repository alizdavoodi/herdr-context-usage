package omp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/transcript"
)

// omp writes whole tool results into the transcript — the largest single record
// across the five real transcripts is 60,190 bytes, a toolResult — so the bound
// has to hold a run of those and still reach the assistant record underneath.
// 8 MiB leaves room for roughly a hundred and thirty of them while keeping the
// scan independent of how long the agent session has run.
const maxScanBytes int64 = 8 << 20

// maxHeaderRecords bounds the search for the session record.
//
// It is not the first record. omp writes a fixed-width title record ahead of it
// — padded so the title can be rewritten in place — which puts the session
// record at line 2 on all five real transcripts. The most header records
// observed ahead of a transcript's first message is 7: title, session, then a
// run of model_change and thinking_level_change records. 16 is a little over
// twice that, and exists so that a transcript carrying no session record cannot
// turn the search into a scan of the whole bounded head.
const maxHeaderRecords = 16

var (
	ErrNoOccupancy   = errors.New("omp transcript has no current context occupancy")
	ErrJustCompacted = fmt.Errorf("omp %w", provider.ErrJustCompacted)
)

type sessionRecord struct {
	Type    string `json:"type"`
	Version *int   `json:"version"`
}

// transcriptEntry decodes only what the walk decides on, and the fields it
// leaves out are the point. Each of the three below is a real field of a real
// omp record, named here with the number that disqualified it so that a later
// reader does not reintroduce one because its name looks right.
//
// There is no totalTokens field: omp's per-turn total is the occupancy plus that
// turn's output, 134,693 against the correct 132,098 on the newest completed
// turn before the one measured compaction. On codex the equivalently named field
// is the one to prefer, for a reason particular to codex, so neither provider's
// rule may be copied to the other.
//
// omp's own log settles why that field is so inviting, and it was read once as
// evidence rather than as an interface. Each turn logged an auto-compaction
// decision carrying a contextWindow of 1,000,000, a thresholdTokens of 850,000
// and a resolvedContextTokens — and 20 of the 31 distinct values of the last
// coincided exactly with a transcript totalTokens while not one coincided with a
// promptTokens, the compaction point among them at 134,693. So omp answers "when
// should I compact" with the per-turn total and this plugin answers "how much is
// resident now" with the prompt size: two questions, two numbers, and the
// tempting field belongs to the other one.
//
// Those per-process logs rotate, and the ones behind that reading are already
// gone from the machine, so unlike every other number in this file it cannot be
// re-checked. Its transcript-side half can: tokensBefore and the previous turn's
// totalTokens are both 134,693, which the fixtures pin. The reason to keep the
// rest anyway is that it also establishes where omp's window limit lives — in
// the log, not the transcript — so this reader could not read the limit at
// runtime even if ADR-0001 had not declined to display it.
//
// There is no tokensBefore field: omp's compaction record carries 134,693, which
// is exactly that same per-turn total, and it describes the agent session before
// the compaction — wrong twice over. That the two numbers are equal is measured,
// not assumed.
//
// There is no nonMessageTokens field, and its name is the whole trap. It is an
// independently computed overhead estimate, recomputed per turn, and it was
// measured both above and below the prompt size: below on 174 turns, and above
// on 4, where 50,213 sat over a prompt size of 48,928. No subset relation holds
// in either direction, so it can be neither added nor subtracted.
//
// There is no reasoningTokens field, which is omp's name for the field pi calls
// reasoning: 557 on the newest turn of the compacted transcript, against that
// turn's output of 931. omp writes it on exactly three turns across the five
// transcripts, all of them deepseek-v4-flash rather than the ollama-cloud turns
// around them — 169 against an output of 296, 619 against 798, and 557 against
// 931 — so it is a subset of the output on every row that carries it, which is
// the same disqualification pi records against its own spelling.
//
// contextSnapshot.promptTokens is decoded, and it is the one field here that is
// read without being reported. It is an oracle, never the source: see
// oracleDisagreement.
type transcriptEntry struct {
	Type    string `json:"type"`
	Message *struct {
		Role  string `json:"role"`
		Usage *struct {
			Input      int64 `json:"input"`
			CacheRead  int64 `json:"cacheRead"`
			CacheWrite int64 `json:"cacheWrite"`
		} `json:"usage"`
		ContextSnapshot *struct {
			PromptTokens *int64 `json:"promptTokens"`
		} `json:"contextSnapshot"`
	} `json:"message"`
}

func readOccupancy(path string) (provider.Reading, error) {
	scan, err := transcript.Read("omp", path, maxScanBytes)
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
			// Stopping keys on the record type, and on omp there is no usage on
			// this record to stop on instead — a walk looking for the newest
			// record holding a usage would sail past it to the last
			// pre-compaction turn and report 132,098 for an agent session whose
			// true occupancy was 40,710 one turn later. That is a 3.2x
			// overstatement: a nearly full window reported for an agent session
			// a third full. pi punishes the same missing stop in the opposite
			// direction, reporting a plausibly small number instead, which is
			// why the two detectors stay in separate packages.
			return provider.Reading{TranscriptVersion: version}, ErrJustCompacted
		}
		if entry.Message == nil || entry.Message.Role != "assistant" || entry.Message.Usage == nil {
			// The gate is the role plus a usage, never stopReason. All 179
			// assistant records across the five transcripts carry a usage, under
			// three stop reasons — toolUse on 158, stop on 20, aborted on 1 — and
			// the newest of the two completing ones is the current window. This
			// count is omp's own and not pi's: pi has two, and transplanting its
			// sentence here without re-measuring is the hazard ADR-0006's
			// consequences section exists to prevent.
			//
			// aborted is the interesting one, because it is the case where
			// "completed" does real work, and it is excluded by arithmetic rather
			// than by a stopReason gate: the one aborted row sums to 0 and carries
			// no contextSnapshot, so the all-zero rejection in provider.Occupancy
			// drops it and the walk reaches the previous completed turn. omp 18.1.2
			// adds a fourth reason, error, and its two rows sum to 0 as well, so
			// the same rejection covers it without this reader learning a new
			// name. A gate on stopReason would instead have to be extended for
			// every reason omp invents, and would discard a genuinely resident
			// prompt if one of them ever carried non-zero input.
			//
			// user and toolResult records carry no usage at all, and the title,
			// session, model_change, thinking_level_change, custom and
			// custom_message records carry no message field either.
			continue
		}
		usage := entry.Message.Usage
		occupancy, ok := provider.Occupancy(usage.Input, usage.CacheRead, usage.CacheWrite)
		if !ok {
			continue
		}
		return provider.Reading{
			Occupancy:          occupancy,
			TranscriptVersion:  version,
			OracleDisagreement: oracleDisagreement(entry, occupancy),
		}, nil
	}
	return provider.Reading{TranscriptVersion: version}, ErrNoOccupancy
}

// oracleDisagreement compares omp's own record of the prompt size against the
// occupancy this reader computed, and describes the difference when there is
// one.
//
// omp records contextSnapshot.promptTokens directly, and it matched the sum
// exactly on all 178 turns across the five real transcripts that carry a
// snapshot, including both turns whose cacheRead was non-zero. Reading it
// instead of summing would be the obvious simplification and is refused on
// purpose: the sum is one formula backed by measurement on two agents, where
// this field exists on one, so promoting it would give omp arithmetic that pi's
// tests cannot corroborate. Kept as an oracle it costs nothing and catches a
// change in omp's usage fields that the sum alone would absorb silently. The
// description rides on the reading beside the sum and never withholds it: a
// value there is a fact about omp's format worth surfacing, not a reason to
// blank the row.
//
// An absent snapshot is not a disagreement. The one real all-zero usage row
// carries no snapshot at all, and a turn omp declined to describe says nothing
// about the arithmetic.
func oracleDisagreement(entry transcriptEntry, occupancy int64) string {
	snapshot := entry.Message.ContextSnapshot
	if snapshot == nil || snapshot.PromptTokens == nil || *snapshot.PromptTokens == occupancy {
		return ""
	}
	return fmt.Sprintf(
		"omp reports contextSnapshot.promptTokens %d where input + cacheRead + cacheWrite is %d; the sum is what was reported",
		*snapshot.PromptTokens, occupancy,
	)
}

// readVersion reads the schema version off the transcript's session record.
//
// The version never gates a measurement. omp and pi both stamp version 3 while
// carrying different field sets, so it distinguishes neither the two agents nor
// a breaking change within one: gating on it would reject a harmless increment
// and accept a breaking change at the same number. It is carried into the
// reading instead, so a format change in a floating dependency shows up in the
// output rather than only as a blank row.
//
// The search walks the head rather than reading one line, and the type check is
// what makes that safe. omp's line 1 is a title record carrying "v": 1 — a
// different key at a different position with a different meaning — so a reader
// that took the first line and looked for a version would find none, and one
// that accepted any version-shaped key there would report 1 for every omp agent
// session. A head with no session record in it leaves the version nil and the
// walk proceeds.
func readVersion(head [][]byte) *int {
	for i, line := range head {
		if i >= maxHeaderRecords {
			return nil
		}
		var record sessionRecord
		if json.Unmarshal(bytes.TrimSpace(line), &record) != nil {
			continue
		}
		if record.Type == "session" {
			return record.Version
		}
	}
	return nil
}
