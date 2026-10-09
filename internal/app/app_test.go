package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

type fakeHerdr struct {
	mu sync.Mutex

	agents       []herdr.Agent
	listErr      error
	panes        []herdr.Pane
	paneListErr  error
	processes    map[string][]provider.Process
	processErrs  map[string]error
	processCalls []string
	reports      map[string][2]string
	clears       []string
	clearErrs    map[string]error
	seqs         []int64
	listAgentsAt int64
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (f *fakeHerdr) ListAgents(context.Context) ([]herdr.Agent, error) {
	f.listAgentsAt = time.Now().UnixNano()
	// The wall clock can be coarser than a nanosecond, so the pause is what
	// guarantees every timestamp taken after the snapshot is strictly later.
	time.Sleep(time.Millisecond)
	return f.agents, f.listErr
}

func (f *fakeHerdr) ListPanes(context.Context) ([]herdr.Pane, error) {
	return f.panes, f.paneListErr
}

// The methods below are called from parallel pane refreshes inside Refresh;
// the mutex mirrors the production client, whose calls are independent
// subprocess spawns.
func (f *fakeHerdr) ProcessInfo(_ context.Context, paneID string) ([]provider.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.processCalls = append(f.processCalls, paneID)
	return f.processes[paneID], f.processErrs[paneID]
}

func (f *fakeHerdr) Report(_ context.Context, paneID, display, raw string, seq int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seqs = append(f.seqs, seq)
	if f.reports == nil {
		f.reports = make(map[string][2]string)
	}
	f.reports[paneID] = [2]string{display, raw}
	return nil
}

func (f *fakeHerdr) Clear(_ context.Context, paneID string, seq int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seqs = append(f.seqs, seq)
	f.clears = append(f.clears, paneID)
	return f.clearErrs[paneID]
}

// fakeProvider stands in for any agent's provider, so the round's policy is
// pinned by what Refresh does with an answer rather than by how one provider
// arrives at it, and no test here needs a store on disk.
type fakeProvider struct {
	reading    provider.Reading
	err        error
	superseded []error

	mu    sync.Mutex
	panes []provider.Pane
}

func (f *fakeProvider) Superseded() []error { return f.superseded }

func (f *fakeProvider) Measure(_ context.Context, pane provider.Pane) (provider.Reading, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panes = append(f.panes, pane)
	return f.reading, f.err
}

func (f *fakeProvider) measured() []provider.Pane {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.panes)
}

// measuring answers 92,481 tokens, the design document's worked example, so a
// report of 92k/92481 is recognisably this provider's reading.
func measuring() *fakeProvider {
	return &fakeProvider{reading: provider.Reading{Occupancy: 92481}}
}

var errSuperseded = errors.New("superseded session")

// measurements decodes the round's log and keeps only the measurement records,
// so an assertion reads a field rather than matching a substring of the JSON.
func measurements(t *testing.T, log *bytes.Buffer) []Measurement {
	t.Helper()

	var records []Measurement
	decoder := json.NewDecoder(bytes.NewReader(log.Bytes()))
	for {
		var record Measurement
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			return records
		} else if err != nil {
			t.Fatalf("decode log %q: %v", log.String(), err)
		}
		if record.Type == "measurement" {
			records = append(records, record)
		}
	}
}

// rawMeasurement is the one measurement record a round wrote, field by field as
// it went over the wire. Decoding into Measurement cannot tell a field written
// as null from one left out, and the record's consumers can: null is the
// contract for "known to be unknown", so presence has to be asserted on the raw
// JSON.
func rawMeasurement(t *testing.T, log *bytes.Buffer) map[string]json.RawMessage {
	t.Helper()

	var found []map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(log.Bytes()))
	for {
		var record map[string]json.RawMessage
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode log %q: %v", log.String(), err)
		}
		if string(record["type"]) == `"measurement"` {
			found = append(found, record)
		}
	}
	if len(found) != 1 {
		t.Fatalf("log = %s, want exactly one measurement record", log.String())
	}
	return found[0]
}

func TestRefreshReturnsListError(t *testing.T) {
	t.Parallel()

	want := errors.New("list failed")
	if err := Refresh(context.Background(), &fakeHerdr{listErr: want}, "", nil, nil); !errors.Is(err, want) {
		t.Fatalf("Refresh() error = %v, want %v", err, want)
	}
}

func TestRefreshFansOutAndClearsEventPaneAfterAgentExit(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{
		agents: []herdr.Agent{
			{Agent: "claude", PaneID: "w1:p1"},
			{Agent: "codex", PaneID: "w1:p2", Tokens: map[string]string{"ctx_raw": "70000"}},
		},
		panes: []herdr.Pane{
			{PaneID: "w1:p1", Tokens: map[string]string{"ctx": "92k", "ctx_raw": "92481"}},
			{PaneID: "w1:exited", Tokens: map[string]string{"ctx": "80k", "ctx_raw": "80000"}},
			{PaneID: "w1:also-exited", Tokens: map[string]string{"ctx_raw": "70000"}},
			{PaneID: "w1:shell"},
		},
	}
	providers := map[provider.Kind]provider.Provider{
		provider.Claude: measuring(),
		provider.Codex:  &fakeProvider{err: errSuperseded, superseded: []error{errSuperseded}},
	}

	if err := Refresh(context.Background(), fake, "w1:exited", nil, providers); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(fake.processCalls) != 2 {
		t.Fatalf("ProcessInfo called for %v, want every live agent", fake.processCalls)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("claude report = %v", got)
	}
	if got, ok := fake.reports["w1:p2"]; ok {
		t.Fatalf("codex report = %v, want no report for a superseded pane", got)
	}
	// The sweep runs before any pane is measured, so its clears come first.
	wantClears := []string{"w1:exited", "w1:also-exited", "w1:p2"}
	if !slices.Equal(fake.clears, wantClears) {
		t.Fatalf("clears = %v, want every stale occupancy pane %v", fake.clears, wantClears)
	}
}

// opencode is the one agent whose classification needs the process list, so a
// transient process-info failure there waits a round: neither version is
// measured, the value is preserved, the run does not fail, and the failure
// lands as a diagnostic.
func TestRefreshPreservesOpenCodeValueWhenProcessInfoFails(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{
		agents:      []herdr.Agent{{Agent: "opencode", PaneID: "w1:p1", Tokens: map[string]string{"ctx_raw": "92481"}}},
		processErrs: map[string]error{"w1:p1": errors.New("process info failed")},
	}
	v1, v2 := measuring(), measuring()
	providers := map[provider.Kind]provider.Provider{provider.OpenCode: v1, provider.OpenCode2: v2}
	var log bytes.Buffer
	if err := Refresh(context.Background(), fake, "", &log, providers); err != nil {
		t.Fatalf("Refresh() error = %v, want nil", err)
	}
	if len(v1.measured()) != 0 || len(v2.measured()) != 0 {
		t.Fatalf("measured v1 %v, v2 %v; want an unclassifiable pane skipped", v1.measured(), v2.measured())
	}
	if len(fake.clears) != 0 || len(fake.reports) != 0 {
		t.Fatalf("clears = %v, reports = %v; want transient process failure to preserve the value", fake.clears, fake.reports)
	}
	if got := log.String(); !strings.Contains(got, `"operation":"process_info"`) {
		t.Fatalf("log = %s, want process-info maintenance diagnostic", got)
	}
}

// An unrecognised agent name is already unmeasurable, so a transient
// process-info failure must not stop its stale occupancy metadata from being
// cleared.
func TestRefreshClearsUnknownAgentWhenProcessInfoFails(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{
		agents:      []herdr.Agent{{Agent: "shell", PaneID: "w1:p1", Tokens: map[string]string{"ctx_raw": "92481"}}},
		processErrs: map[string]error{"w1:p1": errors.New("process info failed")},
	}
	var log bytes.Buffer
	if err := Refresh(context.Background(), fake, "", &log, nil); err != nil {
		t.Fatalf("Refresh() error = %v, want nil", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:p1"}) {
		t.Fatalf("clears = %v, want stale metadata cleared", fake.clears)
	}
}

// Every kind but opencode is classified from its label alone, so a transient
// process-info failure degrades the read to an empty process list instead of
// dropping the pane for the round; a provider that uses process ids as a tier
// falls back to its others.
func TestRefreshMeasuresWithoutAProcessListWhenProcessInfoFails(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{
		agents:      []herdr.Agent{{Agent: "claude", PaneID: "w1:p1"}},
		processErrs: map[string]error{"w1:p1": errors.New("process info failed")},
	}
	claude := measuring()
	var log bytes.Buffer
	if err := Refresh(context.Background(), fake, "", &log, map[provider.Kind]provider.Provider{provider.Claude: claude}); err != nil {
		t.Fatalf("Refresh() error = %v, want degraded measurement to succeed", err)
	}
	if got := claude.measured(); len(got) != 1 || got[0].Processes != nil {
		t.Fatalf("measured panes = %#v, want one with no process list", got)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want measurement despite process-info failure", got)
	}
	if len(fake.clears) != 0 {
		t.Fatalf("clears = %v, want none", fake.clears)
	}
	if got := log.String(); !strings.Contains(got, `"operation":"process_info"`) {
		t.Fatalf("log = %s, want process-info maintenance diagnostic", got)
	}
}

// The opencode label hosts both versions, and the foreground process is what
// tells them apart, so a v2 pane must reach the v2 provider and never be read
// as v1.
func TestRefreshDoesNotClaimOpenCode2AsV1(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{
		agents:    []herdr.Agent{{Agent: "opencode", PaneID: "w1:p1"}},
		processes: map[string][]provider.Process{"w1:p1": {{PID: 42, Argv0: ".OpenCode2"}}},
	}
	v1, v2 := measuring(), measuring()
	providers := map[provider.Kind]provider.Provider{provider.OpenCode: v1, provider.OpenCode2: v2}
	if err := Refresh(context.Background(), fake, "", nil, providers); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(v1.measured()) != 0 || len(v2.measured()) != 1 {
		t.Fatalf("measured v1 %d times, v2 %d times; want the pane measured by v2 only", len(v1.measured()), len(v2.measured()))
	}
}

// A round cut short by its deadline must not report success just because the
// panes that did measure came back clean.
func TestRefreshReportsDeadlineCancelation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fakeHerdr{
		agents: []herdr.Agent{{Agent: "shell", PaneID: "w1:p1"}},
	}
	if err := Refresh(ctx, fake, "", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Refresh() error = %v, want context canceled", err)
	}
}

func TestRefreshReportsASuccessfulReadingAndRecordsItMeasured(t *testing.T) {
	t.Parallel()

	version := 3
	fake := &fakeHerdr{agents: []herdr.Agent{{Agent: "claude", PaneID: "w1:p1"}}}
	claude := &fakeProvider{reading: provider.Reading{
		Mode:               provider.ResolutionProcessRegistry,
		Occupancy:          92481,
		ModelID:            "claude-opus-5-5",
		TranscriptVersion:  &version,
		OracleDisagreement: "store says 92000",
	}}
	var log bytes.Buffer
	if err := Refresh(context.Background(), fake, "", &log, map[provider.Kind]provider.Provider{provider.Claude: claude}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want the formatted and raw occupancy", got)
	}
	records := measurements(t, &log)
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one", records)
	}
	got := records[0]
	if got.PaneID != "w1:p1" || got.Provider != provider.Claude || got.ContextState != ContextMeasured ||
		got.Occupancy == nil || *got.Occupancy != 92481 ||
		got.ResolutionMode == nil || *got.ResolutionMode != provider.ResolutionProcessRegistry ||
		got.ModelID != "claude-opus-5-5" || got.OracleDisagreement != "store says 92000" || got.Error != "" ||
		got.TranscriptVersion == nil || *got.TranscriptVersion != version {
		t.Fatalf("record = %#v, want a measured claude reading carrying its occupancy, mode, model, transcript version and oracle disagreement", got)
	}
}

// A failed reading is classified by the shared lifecycle roots plus the
// provider's own extra declarations: a shared root or a declared extra clears
// the pane, anything else keeps the value standing, and either way the record
// carries whatever the provider observed before failing.
func TestRefreshRecordsAFailedReadingAndClearsOnlyWhatTheProviderDeclares(t *testing.T) {
	t.Parallel()

	version := 3
	undeclared := errors.New("permission denied")
	// Each shared outcome is wrapped once more, so the skeleton has to see
	// through an error chain, and each arrives with no provider extras at all.
	noSession := fmt.Errorf("resolve: %w", provider.ErrNoSession)
	noHome := fmt.Errorf("home: %w", provider.ErrNoHome)
	noOccupancy := fmt.Errorf("read: %w", provider.ErrNoOccupancy)
	compacted := fmt.Errorf("transcript: %w", provider.ErrJustCompacted)
	for _, test := range []struct {
		name       string
		reading    provider.Reading
		err        error
		superseded []error
		wantClear  bool
		wantState  ContextState
		wantMode   provider.ResolutionMode
	}{
		{
			name:       "a declared extra clears",
			err:        fmt.Errorf("resolve: %w", errSuperseded),
			superseded: []error{errSuperseded},
			wantClear:  true,
			wantState:  ContextUnknown,
		},
		{
			name:       "an undeclared error keeps the value",
			reading:    provider.Reading{Mode: provider.ResolutionAgentSession},
			err:        undeclared,
			superseded: []error{errSuperseded},
			wantState:  ContextUnknown,
			wantMode:   provider.ResolutionAgentSession,
		},
		{
			name:      "a shared no-session outcome clears with no extras",
			err:       noSession,
			wantClear: true,
			wantState: ContextUnknown,
		},
		{
			name:      "a shared no-home outcome clears with no extras",
			err:       noHome,
			wantClear: true,
			wantState: ContextUnknown,
		},
		{
			name:      "a shared no-occupancy outcome clears with no extras",
			err:       noOccupancy,
			wantClear: true,
			wantState: ContextUnknown,
		},
		{
			name:      "a shared just-compacted outcome is recorded as such with no extras",
			reading:   provider.Reading{Mode: provider.ResolutionTerminalTitle, TranscriptVersion: &version},
			err:       compacted,
			wantClear: true,
			wantState: ContextJustCompacted,
			wantMode:  provider.ResolutionTerminalTitle,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeHerdr{agents: []herdr.Agent{{Agent: "pi", PaneID: "w1:p1", Tokens: map[string]string{"ctx_raw": "92481"}}}}
			pi := &fakeProvider{reading: test.reading, err: test.err, superseded: test.superseded}
			var log bytes.Buffer
			if err := Refresh(context.Background(), fake, "", &log, map[provider.Kind]provider.Provider{provider.Pi: pi}); err != nil {
				t.Fatalf("Refresh() error = %v; a failed reading is round-local", err)
			}
			if len(fake.reports) != 0 {
				t.Fatalf("reports = %v, want none for a failed reading", fake.reports)
			}
			if cleared := slices.Equal(fake.clears, []string{"w1:p1"}); cleared != test.wantClear || (!cleared && len(fake.clears) != 0) {
				t.Fatalf("clears = %v, want cleared = %v", fake.clears, test.wantClear)
			}
			records := measurements(t, &log)
			if len(records) != 1 {
				t.Fatalf("records = %#v, want one", records)
			}
			got := records[0]
			if got.ContextState != test.wantState || got.Occupancy != nil || got.Error != test.err.Error() {
				t.Fatalf("record = %#v, want state %q, null occupancy and error %q", got, test.wantState, test.err)
			}
			// A zero mode is null rather than an empty name; one that resolved
			// before the read failed is still recorded.
			if test.wantMode == "" && got.ResolutionMode != nil || test.wantMode != "" && (got.ResolutionMode == nil || *got.ResolutionMode != test.wantMode) {
				t.Fatalf("resolution_mode = %v, want %q (null when empty)", got.ResolutionMode, test.wantMode)
			}
			if test.reading.TranscriptVersion != nil && (got.TranscriptVersion == nil || *got.TranscriptVersion != version) {
				t.Fatalf("transcript_version = %v, want %d recorded on the failed read", got.TranscriptVersion, version)
			}
			raw := rawMeasurement(t, &log)
			if occupancy, ok := raw["occupancy"]; !ok || string(occupancy) != "null" {
				t.Fatalf("record = %s, want occupancy written as null rather than left out", log.String())
			}
			if mode, ok := raw["resolution_mode"]; test.wantMode == "" && (!ok || string(mode) != "null") {
				t.Fatalf("record = %s, want an unresolved mode written as null rather than left out", log.String())
			}
		})
	}
}

// paneOf is the only place herdr's record becomes a provider's input, so every
// field a provider resolves from has to arrive intact.
func TestRefreshHandsTheProviderWhatHerdrReportedAboutThePane(t *testing.T) {
	t.Parallel()

	session := &provider.AgentSession{Kind: "id", Value: "ses_current"}
	processes := []provider.Process{{PID: 42, Argv0: "codex", Argv: []string{"codex", "resume"}}}
	fake := &fakeHerdr{
		agents: []herdr.Agent{{
			Agent: "codex", PaneID: "w1:p1",
			ForegroundCWD: "/work/acme", TerminalTitle: "codex | acme", AgentSession: session,
		}},
		processes: map[string][]provider.Process{"w1:p1": processes},
	}
	codex := measuring()
	if err := Refresh(context.Background(), fake, "", nil, map[provider.Kind]provider.Provider{provider.Codex: codex}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	want := provider.Pane{ForegroundCWD: "/work/acme", TerminalTitle: "codex | acme", AgentSession: session, Processes: processes}
	if got := codex.measured(); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("measured panes = %#v, want %#v", got, want)
	}
}

// A kind nobody registered cannot be read, so a value it displays belongs to
// no session this plugin can vouch for and is cleared; a pane showing nothing
// is left alone, and neither produces a measurement record.
func TestRefreshClearsAnUnregisteredKindOnlyWhenItDisplaysOccupancy(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{agents: []herdr.Agent{
		{Agent: "gemini", PaneID: "w1:shown", Tokens: map[string]string{"ctx": "50k"}},
		{Agent: "gemini", PaneID: "w1:blank"},
	}}
	var log bytes.Buffer
	if err := Refresh(context.Background(), fake, "", &log, map[provider.Kind]provider.Provider{provider.Claude: measuring()}); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if !slices.Equal(fake.clears, []string{"w1:shown"}) {
		t.Fatalf("clears = %v, want only the pane displaying occupancy", fake.clears)
	}
	if records := measurements(t, &log); len(records) != 0 {
		t.Fatalf("records = %#v, want none for an unregistered kind", records)
	}
}

func TestRefreshReportsOccupancyWhenMeasurementLoggingFails(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("stdout failed")
	fake := &fakeHerdr{agents: []herdr.Agent{{Agent: "claude", PaneID: "w1:p1"}}}
	err := Refresh(context.Background(), fake, "", errorWriter{err: writeErr}, map[provider.Kind]provider.Provider{provider.Claude: measuring()})
	if !errors.Is(err, writeErr) {
		t.Fatalf("Refresh() error = %v, want logging error %v", err, writeErr)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want occupancy despite logging failure", got)
	}
}

func TestRefreshReportsPaneListFailureWithoutMaskingMeasurement(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		agents:      []herdr.Agent{{Agent: "claude", PaneID: "w1:p1"}},
		paneListErr: errors.New("pane list failed"),
	}
	if err := Refresh(context.Background(), fake, "w1:exited", &log, map[provider.Kind]provider.Provider{provider.Claude: measuring()}); err != nil {
		t.Fatalf("Refresh() error = %v, want successful measurement", err)
	}
	if got := log.String(); !strings.Contains(got, `"type":"maintenance"`) || !strings.Contains(got, `"type":"measurement"`) || !strings.Contains(got, `"operation":"list_panes"`) || !strings.Contains(got, `"error":"pane list failed"`) {
		t.Fatalf("diagnostic = %s, want pane-list failure", got)
	}
	// The exited pane is cleared as the fallback even though another pane is
	// live, which is the ordinary case: one agent exits beside its neighbours.
	if !slices.Equal(fake.clears, []string{"w1:exited"}) {
		t.Fatalf("clears = %v, want event-pane fallback", fake.clears)
	}
	if got := fake.reports["w1:p1"]; got != [2]string{"92k", "92481"} {
		t.Fatalf("report = %v, want successful measurement", got)
	}
}

func TestRefreshReportsEveryStaleMetadataClearFailure(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		panes: []herdr.Pane{
			{PaneID: "w1:p1", Tokens: map[string]string{"ctx": "92k"}},
			{PaneID: "w1:p2", Tokens: map[string]string{"ctx_raw": "92481"}},
		},
		clearErrs: map[string]error{
			"w1:p1": errors.New("first clear failed"),
			"w1:p2": errors.New("second clear failed"),
		},
	}
	if err := Refresh(context.Background(), fake, "", &log, nil); err != nil {
		t.Fatalf("Refresh() error = %v, want sweep failures to preserve exit status", err)
	}
	got := log.String()
	for _, want := range []string{
		`"type":"maintenance"`,
		`"operation":"clear_stale_metadata"`,
		`"pane_id":"w1:p1"`,
		`"error":"first clear failed"`,
		`"pane_id":"w1:p2"`,
		`"error":"second clear failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic = %s, want %s", got, want)
		}
	}
}

func TestRefreshReportsEventPaneFallbackClearFailure(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer
	fake := &fakeHerdr{
		paneListErr: errors.New("pane list failed"),
		clearErrs:   map[string]error{"w1:exited": errors.New("fallback clear failed")},
	}
	if err := Refresh(context.Background(), fake, "w1:exited", &log, nil); err != nil {
		t.Fatalf("Refresh() error = %v, want sweep failures to preserve exit status", err)
	}
	got := log.String()
	for _, want := range []string{
		`"operation":"list_panes"`,
		`"operation":"clear_stale_metadata"`,
		`"pane_id":"w1:exited"`,
		`"error":"fallback clear failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic = %s, want %s", got, want)
		}
	}
}

// Every write in a round is a verdict on that round's agent snapshot, so all of
// them carry one sequence no later than the snapshot, and a later round's is
// greater: whichever round lands last, the one that saw the newer agent list
// wins, for a measurement and a clear alike.
func TestRefreshSequencesEveryWriteByTheRoundsAgentSnapshot(t *testing.T) {
	t.Parallel()

	fake := &fakeHerdr{
		agents: []herdr.Agent{
			{Agent: "claude", PaneID: "w1:p1"},
			{Agent: "gemini", PaneID: "w1:g", Tokens: map[string]string{"ctx": "50k", "ctx_raw": "50000"}},
		},
		panes: []herdr.Pane{{PaneID: "w1:gone", Tokens: map[string]string{"ctx": "80k"}}},
	}
	providers := map[provider.Kind]provider.Provider{provider.Claude: measuring()}
	var previous int64
	for round := range 2 {
		fake.seqs = nil
		if err := Refresh(context.Background(), fake, "", nil, providers); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		// The stale pane, the unregistered kind, and the measured claude pane.
		if len(fake.seqs) != 3 {
			t.Fatalf("round %d seqs = %v, want two clears and a report", round, fake.seqs)
		}
		seq := fake.seqs[0]
		for _, got := range fake.seqs {
			if got != seq || got <= previous || got > fake.listAgentsAt {
				t.Fatalf("round %d seqs = %v, want one value above %d and at or below the agent snapshot %d", round, fake.seqs, previous, fake.listAgentsAt)
			}
		}
		previous = seq
	}
}
