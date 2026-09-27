package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// Provenance for every fixture in this file. All of it derives from the two real
// pi transcripts on the machine this provider was built against, taken with pi
// 0.84.2 and herdr 0.8.0. The compacted one is
//
//	~/.pi/agent/sessions/--Users-alirezadavoodi-projects-github.com-alizdavoodi-herdr-context-usage--/
//	2026-08-18T12-34-48-971Z_01a014de-020a-70b2-bc8e-b6a70ed3e784.jsonl
//
// which is 42 lines long. Positions matter as much as shapes here, so each one
// is named where it is used:
//
//	line 1  the session record, carrying version 3
//	line 6  the first assistant turn, cacheRead 384
//	line 39 the newest assistant turn before the compaction, occupancy 27,905
//	line 40 the compaction record, carrying its own usage
//	line 42 the first assistant turn after the compaction, occupancy 17,033
//
// Lines 41 and 42 exist because they were driven into that agent session to
// close the one reading this provider would otherwise have inferred; before
// that, line 40 was the newest record in the file.
//
// These records are reduced further than the ones in
// internal/provider/pi/reader_test.go, which copies the same lines with their
// ids intact and labels them "as written". Nothing here decodes an id, so they
// are dropped; when the two sets disagree, that file is the faithful copy.
//
// The wrapped directory name is pi's real derivation: it wraps the whole
// absolute path in `--` and leaves dots alone, so `github.com` is not slugged.
// Nothing in this provider ever constructs that name; the fixtures use the real
// shape so an id match is exercised against a realistic tree.
const (
	piSessionRecord  = `{"type":"session","version":3,"id":"01a014de-020a-70b2-bc8e-b6a70ed3e784","timestamp":"2026-08-18T12:34:48.971Z","cwd":"/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"}`
	piUserRecord     = `{"id":"0583cca6","type":"message","parentId":"0cdaedbf","timestamp":"2026-08-18T12:35:26.978Z","message":{"role":"user"}}`
	piToolResult     = `{"id":"90c28ddb","type":"message","parentId":"f88124a8","timestamp":"2026-08-18T12:35:30.774Z","message":{"role":"toolResult"}}`
	piFirstTurn      = `{"id":"f88124a8","type":"message","parentId":"0583cca6","timestamp":"2026-08-18T12:35:30.762Z","message":{"role":"assistant","stopReason":"toolUse","usage":{"input":6677,"output":156,"cacheRead":384,"cacheWrite":0,"reasoning":19,"totalTokens":7217}}}`
	piNewestTurn     = `{"id":"00955bc3","type":"message","parentId":"fccb0027","timestamp":"2026-08-18T12:37:34.783Z","message":{"role":"assistant","stopReason":"stop","usage":{"input":769,"output":2553,"cacheRead":27136,"cacheWrite":0,"reasoning":1394,"totalTokens":30458}}}`
	piPostCompaction = `{"id":"37a1085f","type":"message","parentId":"512774df","timestamp":"2026-08-18T14:48:10.047Z","message":{"role":"assistant","stopReason":"stop","usage":{"input":17033,"output":18,"cacheRead":0,"cacheWrite":0,"reasoning":16,"totalTokens":17051}}}`
	piCompaction     = `{"type":"compaction","timestamp":"2026-08-18T12:50:17.206Z","tokensBefore":30458,"usage":{"input":1383,"output":602,"cacheRead":0,"cacheWrite":0,"reasoning":158,"totalTokens":1985},"details":{"readFiles":[],"modifiedFiles":[]},"firstKeptEntryId":"67d66b63","fromHook":false,"summary":"No prior history."}`
	piSessionDir     = "--Users-alirezadavoodi-projects-github.com-alizdavoodi-herdr-context-usage--"
	piTranscriptName = "2026-08-18T12-34-48-971Z_01a014de-020a-70b2-bc8e-b6a70ed3e784.jsonl"
	piSessionID      = "01a014de-020a-70b2-bc8e-b6a70ed3e784"
)

// writePiTranscript lays a transcript out the way pi does, under
// <home>/sessions/<wrapped cwd>/<start timestamp>_<session id>.jsonl, and hands
// back its path so a case can report it either as a path or as an id.
func writePiTranscript(t *testing.T, home, sessionDir, name string, records ...string) string {
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

func piAgent(session *provider.AgentSession, tokens map[string]string) []herdr.Agent {
	return []herdr.Agent{{
		Agent: "pi", PaneID: "w1:p1",
		// ForegroundCWD is deliberately set and deliberately useless: this
		// provider has no working-directory stage, so a case that resolves must
		// resolve from the reported agent session alone.
		ForegroundCWD: "/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage",
		AgentSession:  session, Tokens: tokens,
	}}
}

// The occupancy is line 39's input + cacheRead + cacheWrite: 769 + 27,136 + 0.
// Reading its totalTokens instead would report 30,458, which is that sum plus
// the turn's own output of 2,553.
func TestRefreshReportsPiOccupancyFromAReportedPath(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writePiTranscript(t, home, piSessionDir, piTranscriptName,
		piSessionRecord, piUserRecord, piFirstTurn, piToolResult, piNewestTurn)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  piAgent(&provider.AgentSession{Kind: "path", Value: path}, nil),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{PiCodingAgentDir: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"28k", "27905"} {
		t.Fatalf("report = %v, want the newest turn's occupancy", got)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want a successful measurement preserved", fake.clears)
	}
	for _, want := range []string{
		`"provider":"pi"`, `"context_state":"measured"`, `"occupancy":27905`,
		`"resolution_mode":"agent_session"`, `"transcript_version":3`,
	} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("measurement = %s, want %s", log.String(), want)
		}
	}
	if strings.Contains(log.String(), "30458") {
		t.Fatalf("measurement = %s, want the per-turn total 30458 left unread", log.String())
	}
	// 1,394 is this turn's reasoning count, a subset of its output of 2,553, and
	// 29,299 is that count wrongly added to the occupancy. Forbidding both is what
	// omp's file already does for its nonMessageTokens — the field's own value and
	// the contaminated sum — and the reasoning trap needs the pair for the same
	// reason: reading the field reports 1,394, adding it reports 29,299, and
	// neither number contains the other.
	for _, forbidden := range []string{"1394", "29299"} {
		if strings.Contains(log.String(), forbidden) {
			t.Fatalf("measurement = %s, want the reasoning count %s left unread", log.String(), forbidden)
		}
	}
}

// An id is matched against transcript file names under this provider's home. The
// pane's directory takes no part in it, which is why the fixture sits under a
// wrapped directory naming a path the agent no longer runs in.
func TestRefreshResolvesPiSessionFromAReportedID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writePiTranscript(t, home, "--Users-alirezadavoodi--", piTranscriptName,
		piSessionRecord, piUserRecord, piNewestTurn)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  piAgent(&provider.AgentSession{Kind: "id", Value: piSessionID}, nil),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{PiCodingAgentDir: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"28k", "27905"} {
		t.Fatalf("report = %v, want the id matched to its transcript", got)
	}
	if !strings.Contains(log.String(), `"resolution_mode":"agent_session"`) {
		t.Fatalf("measurement = %s, want the resolution mode recorded", log.String())
	}
}

// Both halves of the id contract clear the row rather than guess. The ambiguous
// case is the one worth pinning: two transcripts ending in the same id is
// exactly where a recency tiebreak would look reasonable and be wrong.
func TestRefreshClearsPiRowWhenAnIDDoesNotResolveUniquely(t *testing.T) {
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
				writePiTranscript(t, home, piSessionDir, piTranscriptName,
					piSessionRecord, piNewestTurn)
				writePiTranscript(t, home, "--Users-alirezadavoodi--",
					"2026-08-16T20-45-15-107Z_"+piSessionID+".jsonl",
					piSessionRecord, piFirstTurn)
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
				agents: piAgent(&provider.AgentSession{Kind: "id", Value: piSessionID},
					map[string]string{"ctx": "28k", "ctx_raw": "27905"}),
				reports: make(map[string][2]string),
			}

			if err := Refresh(context.Background(), fake, "", &log, Config{PiCodingAgentDir: home}); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
				t.Fatalf("clears = %v, reports = %v; want an unresolved session blanked", fake.clears, fake.reports)
			}
			if got := log.String(); !strings.Contains(got, "agent session") {
				t.Fatalf("measurement = %q, want the blank reason to name the unresolved session", got)
			}
		})
	}
}

// A pane whose agent has not reported an agent session at all is the case the
// documented integration precondition produces, and it must blank rather than
// keep whatever the pane last showed.
func TestRefreshClearsPiRowWhenNoAgentSessionIsReported(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  piAgent(nil, map[string]string{"ctx_raw": "27905"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{PiCodingAgentDir: t.TempDir()}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want no reported session blanked", fake.clears, fake.reports)
	}
	// No stage resolved anything, so the record says null. A provider leaves its
	// mode empty here and app alone turns that into null; an empty string would
	// read as a mode that exists but has no name.
	if got := log.String(); !strings.Contains(got, `"resolution_mode":null`) {
		t.Fatalf("measurement = %s, want an unresolved pane's resolution mode recorded as null", got)
	}
}

// The trap this whole provider exists to avoid. pi's compaction record carries
// its own usage — the cost of generating the summary — and on the real record
// that usage sums to 1,383. A walk that stopped at the newest record holding a
// usage would report 1,383 for a freshly compacted agent session, which is
// plausible enough that nobody would question it. Walking one record further
// would instead report line 39's 27,905, a pre-compaction occupancy presented as
// current. Neither number may appear.
func TestRefreshClearsPiOccupancyAtTheCompactionBoundary(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writePiTranscript(t, home, piSessionDir, piTranscriptName,
		piSessionRecord, piUserRecord, piNewestTurn, piCompaction)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: piAgent(&provider.AgentSession{Kind: "path", Value: path},
			map[string]string{"ctx": "28k", "ctx_raw": "27905"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{PiCodingAgentDir: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want the pre-compaction occupancy cleared", fake.clears, fake.reports)
	}
	got := log.String()
	if !strings.Contains(got, `"context_state":"just_compacted"`) {
		t.Fatalf("measurement = %q, want just_compacted", got)
	}
	// Resolution succeeded before the read failed, so its mode is still recorded.
	if !strings.Contains(got, `"resolution_mode":"agent_session"`) {
		t.Fatalf("measurement = %q, want the resolution mode kept on the failed read", got)
	}
	// Occupancy and the version arrive from one struct, so hoisting the
	// occupancy assignment above the error check in refreshOne would compile and
	// would publish a zero here. Pinning the null is what fails that edit.
	if !strings.Contains(got, `"occupancy":null`) {
		t.Fatalf("measurement = %q, want no occupancy at a compaction boundary", got)
	}
	// The transcript was read right up to the compaction record, so the version
	// it was read at is known and belongs in the record even though no occupancy
	// does.
	if !strings.Contains(got, `"transcript_version":3`) {
		t.Fatalf("measurement = %q, want the observed version recorded", got)
	}
	for _, forbidden := range []string{"1383", "27905"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("measurement = %q, want %s never reported at a compaction boundary", got, forbidden)
		}
	}
}

// Recovery after a compaction needs no compensating arithmetic once the walk
// stops at the right record: the next turn reports through the ordinary fields,
// with no zeroing of the breakdown.
//
// This was the one link in the chain that had been inferred rather than
// measured, because the only compacted pi agent session available ended at its
// compaction record. A further turn was driven into that same agent session to
// close it, taking the transcript from 40 lines to 42: line 41 the prompt, line
// 42 the assistant turn used verbatim here. Its occupancy is 17,033 + 0 + 0, and
// the per-turn identity held — 17,033 + 18 == 17,051 == totalTokens.
//
// Two things the measurement settled that the inference could not. The
// breakdown is not zeroed and carries no marker of the compaction, so the
// ordinary arithmetic is all that is needed. And cacheRead is 0 on this turn
// while input carries the whole re-sent summary, which is the opposite balance
// from the mid-agent-session turns above — a reader that had come to expect a
// large cacheRead would still be correct here, and this pins that.
func TestRefreshReportsPiOccupancyOnTheTurnAfterACompaction(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writePiTranscript(t, home, piSessionDir, piTranscriptName,
		piSessionRecord, piUserRecord, piNewestTurn, piCompaction, piUserRecord, piPostCompaction)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:  piAgent(&provider.AgentSession{Kind: "path", Value: path}, nil),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{PiCodingAgentDir: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"17k", "17033"} {
		t.Fatalf("report = %v, want the post-compaction turn's occupancy", got)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want the recovered occupancy preserved", fake.clears)
	}
	// 17,051 is this turn's own totalTokens. Reporting it would overstate the
	// occupancy by the turn's output, and a freshly compacted agent session is
	// exactly where an 18-token overstatement would go unnoticed.
	if strings.Contains(log.String(), "17051") {
		t.Fatalf("measurement = %s, want the per-turn total 17051 left unread", log.String())
	}
}

// A read that fails for a reason the provider never claimed says nothing about
// the agent session, so the displayed value stands. Reporting a path that is a
// directory produces exactly that: the open succeeds and the read does not,
// which is the same class of answer as the permission error this guards against.
func TestRefreshPreservesPiValueOnATransientReadFailure(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	unreadable := filepath.Join(home, "sessions", piSessionDir)
	if err := os.MkdirAll(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := &fakeHerdr{
		agents: piAgent(&provider.AgentSession{Kind: "path", Value: unreadable},
			map[string]string{"ctx": "28k", "ctx_raw": "27905"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", nil, Config{PiCodingAgentDir: home}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.clears) != 0 || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want a transient failure to preserve the value", fake.clears, fake.reports)
	}
}

// An agent session that has started but not yet answered has no occupancy to
// report. Blank is the answer; zero would read as an empty window.
func TestRefreshClearsPiRowWhenNoAssistantTurnHasCompleted(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writePiTranscript(t, home, piSessionDir, piTranscriptName,
		piSessionRecord, piUserRecord)
	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: piAgent(&provider.AgentSession{Kind: "path", Value: path},
			map[string]string{"ctx_raw": "27905"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{PiCodingAgentDir: home}); err != nil {
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

// An agent home that cannot be resolved at all must blank the row. Left
// undeclared, the answer would be preserved as any undeclared answer is, and the
// pane would keep a number that was correct once and stays plausible forever,
// for as long as it lived.
//
// Measured against pi 0.84.2 and herdr 0.8.0 by driving this same path with the
// sentinel wrapped but left out of pi's superseded answers: the record read
// context_state "unknown", occupancy null and resolution_mode null, and clears
// was empty, so the pane kept 27,905 from its previous round.
//
// Sets the environment, so it cannot run in parallel. Unsetting HOME defeats
// os.UserHomeDir only on unix; Windows reads USERPROFILE. A pane reporting a path
// resolves without a home and must stay healthy; that is pinned in the provider
// package, because only the id branch reaches a home at all.
func TestRefreshClearsPiRowWhenTheAgentHomeCannotBeResolved(t *testing.T) {
	t.Setenv("HOME", "")

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents: piAgent(&provider.AgentSession{Kind: "id", Value: piSessionID},
			map[string]string{"ctx": "28k", "ctx_raw": "27905"}),
		reports: make(map[string][2]string),
	}

	if err := Refresh(context.Background(), fake, "", &log, Config{}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want an unresolvable home to blank the row", fake.clears, fake.reports)
	}
	// Unknown is null, never zero: a reported 0 would read as an empty window.
	if strings.Contains(log.String(), `"occupancy":0`) {
		t.Fatalf("measurement reported zero occupancy; want null: %s", log.String())
	}
	// The two blanking reasons have different fixes — install the integration
	// against export a home — so the record has to tell them apart. The home
	// answer is a fact about the host and carries no agent's name; the record's
	// provider field is what names the agent.
	if got := log.String(); !strings.Contains(got, "agent home cannot be resolved") ||
		strings.Contains(got, "pi agent session cannot be resolved") {
		t.Fatalf("measurement = %s, want the home named distinguishably from an unresolved agent session", got)
	}
	if got := log.String(); !strings.Contains(got, `"provider":"pi"`) {
		t.Fatalf("measurement = %s, want the provider field to name the agent the home answer does not", got)
	}
}
