package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
)

// Provenance for every fixture in this file. All of it derives from the five
// real omp transcripts on the machine this provider was built against, taken
// with omp 17.3.5 and herdr 0.8.0. The compacted one is
//
//	~/.omp/agent/sessions/-projects-github.com-alizdavoodi-herdr-context-usage/
//	2026-08-18T10-57-53-412Z_01a01485-4504-7000-ae6b-ace797eca8f5.jsonl
//
// which is 272 lines long. Positions matter as much as shapes here, so each one
// is named where it is used:
//
//	line 1   the title record, which is why the session record is not line 1
//	line 2   the session record, carrying version 3
//	line 250 the newest assistant turn before the compaction, occupancy 132,098
//	line 251 a custom record, sitting between that turn and the compaction
//	line 252 the compaction record, carrying no usage and tokensBefore 134,693
//	line 253 the user record that reopened the agent session
//	line 254 the first assistant turn after the compaction, occupancy 40,710
//
// These records are reduced further than the ones in
// internal/provider/omp/reader_test.go, which copies the same lines with their
// ids intact and labels them "as written". Nothing here decodes an id, so most
// are dropped; when the two sets disagree, that file is the faithful copy.
//
// The session directory name is omp's real derivation: it strips the home prefix
// and leaves dots alone, so `github.com` is not slugged and no leading path
// segment survives. pi wraps the whole absolute path instead. Nothing in this
// provider ever constructs that name; the fixtures use the real shape so an id
// match is exercised against a realistic tree.
const (
	ompTitleRecord    = `{"type":"title","v":1,"title":"Review q2, q5, q6, q7 feedback","source":"auto","updatedAt":"2026-08-18T11:13:14.011Z","pad":"    "}`
	ompSessionRecord  = `{"type":"session","version":3,"id":"01a01485-4504-7000-ae6b-ace797eca8f5","timestamp":"2026-08-18T10:57:53.412Z","cwd":"/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"}`
	ompUserRecord     = `{"type":"message","id":"706f5ec7","timestamp":"2026-08-18T12:27:14.473Z","message":{"role":"user","attribution":"user"}}`
	ompCustomRecord   = `{"type":"custom","customType":"session_exit","data":{"reason":"dispose","kind":"normal"},"id":"7f357a96","timestamp":"2026-08-18T11:15:33.801Z"}`
	ompNewestTurn     = `{"type":"message","id":"8a99ff86","timestamp":"2026-08-18T11:15:01.800Z","message":{"role":"assistant","model":"glm-5.2","usage":{"input":132098,"output":2595,"cacheRead":0,"cacheWrite":0,"totalTokens":134693},"stopReason":"stop","contextSnapshot":{"promptTokens":132098,"nonMessageTokens":19016}}}`
	ompPostCompaction = `{"type":"message","id":"32a5030c","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","model":"glm-5.2","usage":{"input":40710,"output":38,"cacheRead":0,"cacheWrite":0,"totalTokens":40748},"stopReason":"stop","contextSnapshot":{"promptTokens":40710,"nonMessageTokens":18981}}}`
	ompCompaction     = `{"type":"compaction","id":"6a93e330","timestamp":"2026-08-18T12:22:29.244Z","summary":"## Goal ...","shortSummary":"I c...","firstKeptEntryId":"12d03f0e","tokensBefore":134693,"details":{"readFiles":[],"modifiedFiles":[]},"fromExtension":false}`

	// Line 254 with promptTokens raised by 1,200 and the counts left alone. The
	// one constructed record here: omp's own prompt size matched the sum exactly
	// on all 178 real turns that carry one, so a disagreement has no precedent.
	// It stands in for a future omp release whose usage fields stop summing to
	// its own prompt size.
	ompOracleDisagrees = `{"type":"message","id":"32a5030c","timestamp":"2026-08-18T12:27:15.759Z","message":{"role":"assistant","model":"glm-5.2","usage":{"input":40710,"output":38,"cacheRead":0,"cacheWrite":0,"totalTokens":40748},"stopReason":"stop","contextSnapshot":{"promptTokens":41910,"nonMessageTokens":18981}}}`

	ompSessionDir     = "-projects-github.com-alizdavoodi-herdr-context-usage"
	ompTranscriptName = "2026-08-18T10-57-53-412Z_01a01485-4504-7000-ae6b-ace797eca8f5.jsonl"
	ompSessionID      = "01a01485-4504-7000-ae6b-ace797eca8f5"
)

// writeOmpTranscript lays a transcript out the way omp does, under
// <home>/sessions/<stripped cwd>/<start timestamp>_<session id>.jsonl, and hands
// back its path so a case can report it either as a path or as an id.
func writeOmpTranscript(t *testing.T, home, sessionDir, name string, records ...string) string {
	t.Helper()

	dir := filepath.Join(home, "sessions", sessionDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(records, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ompAgent(session *herdr.AgentSession, tokens map[string]string) []herdr.Agent {
	return []herdr.Agent{{
		Agent: "omp", PaneID: "w1:p1",
		// ForegroundCWD is deliberately set and deliberately useless: this
		// provider has no working-directory stage, so a case that resolves must
		// resolve from the reported agent session alone.
		ForegroundCWD: "/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage",
		AgentSession:  session, Tokens: tokens,
	}}
}

// The occupancy is line 250's input + cacheRead + cacheWrite: 132,098 + 0 + 0.
// Reading its totalTokens instead would report 134,693, which is that sum plus
// the turn's own output of 2,595.
//
// The exact report is also what pins the window limit out of the output. omp
// knows its own limit — its log records a 1,000,000 window with an 850,000
// auto-compaction threshold on every turn — and ADR-0001 declined to display it
// anyway. A display value of "132k" rather than a fraction, a percentage or a
// gauge is that decision at the boundary a user actually sees.
func TestRefreshReportsOmpOccupancyFromAReportedPath(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writeOmpTranscript(t, home, ompSessionDir, ompTranscriptName,
		ompTitleRecord, ompSessionRecord, ompUserRecord, ompNewestTurn)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  ompAgent(&herdr.AgentSession{Kind: "path", Value: path}, nil),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OmpHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"132k", "132098"} {
		t.Fatalf("report = %v, want the newest turn's occupancy", got)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want a successful measurement preserved", fake.clears)
	}
	for _, want := range []string{
		`"provider":"omp"`, `"context_state":"measured"`, `"occupancy":132098`,
		`"resolution_mode":"agent_session"`, `"transcript_version":3`,
	} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("measurement = %s, want %s", log.String(), want)
		}
	}
	// 134,693 is this turn's own totalTokens and also the compaction record's
	// tokensBefore, so one number covers both fields that must never be read.
	if strings.Contains(log.String(), "134693") {
		t.Fatalf("measurement = %s, want the per-turn total 134693 left unread", log.String())
	}
	// 19,016 is the same record's nonMessageTokens and 151,114 is that count
	// added to the occupancy. It was measured both above and below the prompt
	// size across the five transcripts, so it can be neither added nor
	// subtracted, and neither number may reach the output.
	for _, forbidden := range []string{"19016", "151114"} {
		if strings.Contains(log.String(), forbidden) {
			t.Fatalf("measurement = %s, want the non-message count %s left unread", log.String(), forbidden)
		}
	}
}

// An id is matched against transcript file names under this provider's home. The
// pane's directory takes no part in it, which is why the fixture sits under a
// directory naming a path the agent no longer runs in.
func TestRefreshResolvesOmpSessionFromAReportedID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeOmpTranscript(t, home, "-projects-github.com-alizdavoodi-herdr-context-usage-docs", ompTranscriptName,
		ompTitleRecord, ompSessionRecord, ompUserRecord, ompNewestTurn)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  ompAgent(&herdr.AgentSession{Kind: "id", Value: ompSessionID}, nil),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OmpHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"132k", "132098"} {
		t.Fatalf("report = %v, want the id matched to its transcript", got)
	}
	if !strings.Contains(log.String(), `"resolution_mode":"agent_session"`) {
		t.Fatalf("measurement = %s, want the resolution mode recorded", log.String())
	}
}

// Both halves of the id contract clear the row rather than guess. The ambiguous
// case is the one worth pinning: two transcripts ending in the same id is
// exactly where a recency tiebreak would look reasonable and be wrong.
func TestRefreshClearsOmpRowWhenAnIDDoesNotResolveUniquely(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setUp func(t *testing.T, home string)
	}{
		{
			name:  "no transcript matches the id",
			setUp: func(*testing.T, string) {},
		},
		{
			name: "two transcripts match the id",
			setUp: func(t *testing.T, home string) {
				writeOmpTranscript(t, home, ompSessionDir, ompTranscriptName,
					ompTitleRecord, ompSessionRecord, ompNewestTurn)
				writeOmpTranscript(t, home, "-projects-github.com-alizdavoodi-herdr-context-usage-docs",
					"2026-08-19T09-00-00-000Z_"+ompSessionID+".jsonl",
					ompTitleRecord, ompSessionRecord, ompPostCompaction)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			test.setUp(t, home)
			var log bytes.Buffer
			fake := &fakeHerdr{
				agents: ompAgent(&herdr.AgentSession{Kind: "id", Value: ompSessionID},
					map[string]string{"ctx": "132k", "ctx_raw": "132098"}),
				reports: make(map[string][2]string),
			}

			if err := Refresh(context.Background(), fake, "", &log, Config{OmpHome: home}); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
				t.Fatalf("clears = %v, reports = %v; want an unresolved session blanked", fake.clears, fake.reports)
			}
			// "agent session" alone would also match the just-compacted message,
			// so the reason is pinned to the resolution failure itself.
			if got := log.String(); !strings.Contains(got, "omp agent session cannot be resolved") {
				t.Fatalf("measurement = %q, want the blank reason to name the unresolved session", got)
			}
		})
	}
}

// A pane whose agent has not reported an agent session at all is the case the
// documented integration precondition produces, and on omp that precondition
// bites harder than on pi: omp ships no screen-detection manifest, so before its
// integration is installed the pane is not labelled an agent at all. Whatever
// the cause, the row must blank rather than keep what the pane last showed.
func TestRefreshClearsOmpRowWhenNoAgentSessionIsReported(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  ompAgent(nil, map[string]string{"ctx_raw": "132098"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OmpHome: t.TempDir()}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want no reported session blanked", fake.clears, fake.reports)
	}
}

// The trap this whole provider exists to avoid, and it fails in the opposite
// direction from pi's. omp's compaction record carries no usage of its own, so a
// walk that stopped at the newest record holding a usage would sail past line
// 252 and report line 250's 132,098 — against the 40,710 this agent session
// actually held at the next turn boundary, a 3.2x overstatement. A nearly full
// window for an agent session a third full is not detectable from the value
// alone, which is why detection keys on the record type. 134,693, the
// compaction record's own tokensBefore, must not appear either.
func TestRefreshClearsOmpOccupancyAtTheCompactionBoundary(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writeOmpTranscript(t, home, ompSessionDir, ompTranscriptName,
		ompTitleRecord, ompSessionRecord, ompUserRecord, ompNewestTurn, ompCustomRecord, ompCompaction)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: ompAgent(&herdr.AgentSession{Kind: "path", Value: path},
			map[string]string{"ctx": "132k", "ctx_raw": "132098"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OmpHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want the pre-compaction occupancy cleared", fake.clears, fake.reports)
	}
	got := log.String()
	if !strings.Contains(got, `"context_state":"just_compacted"`) {
		t.Fatalf("measurement = %q, want just_compacted", got)
	}
	// Occupancy and the version arrive from one struct, so hoisting the occupancy
	// assignment above the error check in refreshOne would compile and would
	// publish a zero here. Pinning the null is what fails that edit.
	if !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("measurement = %q, want no occupancy at a compaction boundary", got)
	}
	// The transcript was read right up to the compaction record, so the version
	// it was read at is known and belongs in the record even though no occupancy
	// does.
	if !strings.Contains(got, `"transcript_version":3`) {
		t.Fatalf("measurement = %q, want the observed version recorded", got)
	}
	for _, forbidden := range []string{"132098", "134693"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("measurement = %q, want %s never reported at a compaction boundary", got, forbidden)
		}
	}
}

// Recovery after a compaction needs no compensating arithmetic once the walk
// stops at the right record: the next turn reports through the ordinary fields,
// with no zeroing of the breakdown. omp's recovery was measured twice, so unlike
// pi's this was never an inferred link.
//
// The interesting detail the measurement settled is that the breakdown carries
// no marker of the compaction at all — input simply carries the whole re-sent
// summary and cacheRead is 0, the opposite balance from a long mid-session turn.
// A reader that had come to expect a large cacheRead would still be correct
// here, and this pins that.
func TestRefreshReportsOmpOccupancyOnTheTurnAfterACompaction(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writeOmpTranscript(t, home, ompSessionDir, ompTranscriptName,
		ompTitleRecord, ompSessionRecord, ompUserRecord, ompNewestTurn, ompCustomRecord,
		ompCompaction, ompUserRecord, ompPostCompaction)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  ompAgent(&herdr.AgentSession{Kind: "path", Value: path}, nil),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OmpHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"41k", "40710"} {
		t.Fatalf("report = %v, want the post-compaction turn's occupancy", got)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want the recovered occupancy preserved", fake.clears)
	}
	// 40,748 is this turn's own totalTokens. Reporting it would overstate the
	// occupancy by the turn's output, and a freshly compacted agent session is
	// exactly where a 38-token overstatement would go unnoticed.
	if strings.Contains(log.String(), "40748") {
		t.Fatalf("measurement = %s, want the per-turn total 40748 left unread", log.String())
	}
}

// A read that fails for a reason the provider never claimed says nothing about
// the agent session, so the displayed value stands. Reporting a path that is a
// directory produces exactly that: the open succeeds and the read does not,
// which is the same class of answer as the permission error this guards against.
func TestRefreshPreservesOmpValueOnATransientReadFailure(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	unreadable := filepath.Join(home, "sessions", ompSessionDir)
	if err := os.MkdirAll(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := &fakeHerdr{
		agents: ompAgent(&herdr.AgentSession{Kind: "path", Value: unreadable},
			map[string]string{"ctx": "132k", "ctx_raw": "132098"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", nil, Config{OmpHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 0 || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want a transient failure to preserve the value", fake.clears, fake.reports)
	}
}

// An agent session that has started but not yet answered has no occupancy to
// report. Blank is the answer; zero would read as an empty window.
func TestRefreshClearsOmpRowWhenNoAssistantTurnHasCompleted(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writeOmpTranscript(t, home, ompSessionDir, ompTranscriptName,
		ompTitleRecord, ompSessionRecord, ompUserRecord)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: ompAgent(&herdr.AgentSession{Kind: "path", Value: path},
			map[string]string{"ctx_raw": "132098"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{OmpHome: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want a session with no turn blanked", fake.clears, fake.reports)
	}
	got := log.String()
	if strings.Contains(got, `"occupancy":0`) {
		t.Fatalf("measurement = %q, want unknown reported as null rather than zero", got)
	}
	// The transcript itself was readable, so the record must not read as though
	// the file could not be opened. This is the distinction a maintainer needs
	// when a row is blank: an unresolvable agent session and an unreadable
	// transcript are different faults.
	if !strings.Contains(got, `"transcript_version":3`) {
		t.Fatalf("measurement = %q, want the readable transcript's version recorded", got)
	}
}

// The case that exists only for omp. omp records its own prompt size beside the
// counts this plugin sums, and the two agreed exactly on all 178 real turns that
// carry one. A disagreement therefore says something about omp's format rather
// than about this pane, so it is recorded and the sum is still what gets
// reported. This test is the guard against a later reader quietly promoting the
// oracle to the source: reading 41,910 here would pass every other case in this
// file.
func TestRefreshRecordsOmpOracleDisagreementAndStillReportsTheSum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		turn string
		want [2]string
		// The numbers the record must name, or none when the oracle agrees.
		// Agreement has to stay silent, or the field would appear on every row
		// of every omp pane and mean nothing.
		wantNaming []string
	}{
		{
			// Line 250 as written: promptTokens 132,098 against a sum of 132,098.
			name: "agreement is silent",
			turn: ompNewestTurn,
			want: [2]string{"132k", "132098"},
		},
		{
			// Both numbers belong in the diagnostic so a maintainer can see which
			// way omp moved without opening the transcript.
			name:       "a disagreement is recorded",
			turn:       ompOracleDisagrees,
			want:       [2]string{"41k", "40710"},
			wantNaming: []string{"41910", "40710"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			path := writeOmpTranscript(t, home, ompSessionDir, ompTranscriptName,
				ompTitleRecord, ompSessionRecord, ompUserRecord, tt.turn)
			var log bytes.Buffer
			fake := &fakeHerdr{
				agents:  ompAgent(&herdr.AgentSession{Kind: "path", Value: path}, nil),
				reports: make(map[string][2]string),
			}

			if err := Refresh(context.Background(), fake, "", &log, Config{OmpHome: home}); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if got := fake.reports["w1:p1"]; got != tt.want {
				t.Fatalf("report = %v, want the sum %v reported whatever the oracle says", got, tt.want)
			}
			if len(fake.clears) != 0 {
				t.Fatalf("clears = %v, want a disagreement to diagnose rather than blank the row", fake.clears)
			}
			got := log.String()
			if !strings.Contains(got, `"context_state":"measured"`) {
				t.Fatalf("measurement = %q, want the sum measured and reported", got)
			}
			if len(tt.wantNaming) == 0 {
				if strings.Contains(got, "oracle_disagreement") {
					t.Fatalf("measurement = %q, want no disagreement recorded where the oracle agrees", got)
				}
				return
			}
			// The assertion is scoped to the diagnostic's own field. Searching the
			// whole record would let "40710" pass on the strength of the
			// occupancy field beside it, which is exactly the number the
			// diagnostic is supposed to be carrying itself.
			diagnostic := oracleDisagreementIn(t, got)
			for _, number := range tt.wantNaming {
				if !strings.Contains(diagnostic, number) {
					t.Fatalf("oracle_disagreement = %q, want it to name %s", diagnostic, number)
				}
			}
		})
	}
}

// A pi pane and an omp pane in the same round must each be measured against
// their own agent. The two stores have the same layout below the home and both
// stamp version 3, so a provider reaching for the wrong home would report a
// number rather than fail — which is the whole reason the homes are two
// configuration fields rather than one.
func TestRefreshMeasuresPiAndOmpPanesAgainstTheirOwnStores(t *testing.T) {
	t.Parallel()

	piHome, ompHome := t.TempDir(), t.TempDir()
	piPath := writePiTranscript(t, piHome, piSessionDir, piTranscriptName,
		piSessionRecord, piUserRecord, piNewestTurn)
	ompPath := writeOmpTranscript(t, ompHome, ompSessionDir, ompTranscriptName,
		ompTitleRecord, ompSessionRecord, ompUserRecord, ompNewestTurn)

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: []herdr.Agent{
			{Agent: "pi", PaneID: "w1:p1", AgentSession: &herdr.AgentSession{Kind: "path", Value: piPath}},
			{Agent: "omp", PaneID: "w1:p2", AgentSession: &herdr.AgentSession{Kind: "path", Value: ompPath}},
		},
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{PiHome: piHome, OmpHome: ompHome}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"28k", "27905"} {
		t.Fatalf("pi report = %v, want pi's own newest turn", got)
	}
	if got := fake.reports["w1:p2"]; got != [2]string{"132k", "132098"} {
		t.Fatalf("omp report = %v, want omp's own newest turn", got)
	}
}

// oracleDisagreementIn pulls the diagnostic out of the measurement record so an
// assertion about it cannot be satisfied by a number printed elsewhere in the
// same line.
func oracleDisagreementIn(t *testing.T, log string) string {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		var record struct {
			OracleDisagreement string `json:"oracle_disagreement"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record.OracleDisagreement != "" {
			return record.OracleDisagreement
		}
	}
	t.Fatalf("measurement = %q, want a recorded oracle disagreement", log)
	return ""
}
