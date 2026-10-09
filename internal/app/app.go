package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

type Herdr interface {
	ListAgents(context.Context) ([]herdr.Agent, error)
	ListPanes(context.Context) ([]herdr.Pane, error)
	ProcessInfo(context.Context, string) ([]provider.Process, error)
	Report(context.Context, string, string, string, int64) error
	Clear(context.Context, string, int64) error
}

type Config struct {
	ClaudeHome  string
	CodexHome   string
	OpenCodeDB  string
	OpenCode2DB string
	// OpenCodeDataHome is the data directory whose opencode subdirectory holds
	// both opencode stores when no explicit database path is given, i.e.
	// $XDG_DATA_HOME.
	OpenCodeDataHome string
	// PiCodingAgentDir is PI_CODING_AGENT_DIR. Both pi and omp honour that one
	// variable, so one export redirects both agents. It is passed to each
	// unresolved: an empty value is what lets each provider fall back to its
	// own default home, which keeps an omp pane off pi's store on a machine
	// running both.
	PiCodingAgentDir string
	HermesHome       string
}

type ContextState string

const (
	ContextMeasured      ContextState = "measured"
	ContextUnknown       ContextState = "unknown"
	ContextJustCompacted ContextState = "just_compacted"
)

type Measurement struct {
	Type           string                   `json:"type"`
	PaneID         string                   `json:"pane_id"`
	Provider       provider.Kind            `json:"provider"`
	ContextState   ContextState             `json:"context_state"`
	Occupancy      *int64                   `json:"occupancy"`
	ResolutionMode *provider.ResolutionMode `json:"resolution_mode"`
	ModelID        string                   `json:"model_id,omitempty"`
	// The version is not a compatibility gate — pi and omp stamp the same one
	// while carrying different field sets — so it is carried into the output
	// instead: that is what makes a format change in a floating dependency
	// visible rather than leaving it to surface only as a blank row. omitempty
	// keeps the records of providers whose stores carry no version unchanged.
	TranscriptVersion *int `json:"transcript_version,omitempty"`
	// OracleDisagreement is set when a provider's store carries an independent
	// figure for what this plugin computed and the two stopped agreeing. Only
	// omp has one; it agreed on every turn measured, so a value here is news
	// about the agent's format rather than about this pane. It never withholds a
	// number — the row is still reported — which is the difference between an
	// oracle and a source.
	OracleDisagreement string `json:"oracle_disagreement,omitempty"`
	Error              string `json:"error,omitempty"`
}

type MaintenanceDiagnostic struct {
	Type      string `json:"type"`
	Operation string `json:"operation"`
	PaneID    string `json:"pane_id,omitempty"`
	Error     string `json:"error"`
}

// refreshConcurrency bounds simultaneous pane refreshes. Each pane costs two
// or three herdr subprocess spawns, so a serial sweep stays inside main's 20s
// refreshTimeout only while the pane count is small. A small bound removes the
// N×spawn-latency term from the round without letting parallel spawns swamp
// the machine.
const refreshConcurrency = 4

func Refresh(ctx context.Context, client Herdr, eventPaneID string, log io.Writer, providers map[provider.Kind]provider.Provider) error {
	// Herdr does not serialize overlapping plugin runs and drops a write whose
	// sequence is not above the last one it accepted. Every write in a round is
	// a verdict on this agent snapshot, so the snapshot's time orders them all:
	// a round that saw an agent arrive or leave outranks every older round,
	// whichever lands first. The price is that two rounds measuring the same
	// live pane rank by snapshot, not by read, so a slower round's fresher
	// value can lose and the row lags until the next successful refresh,
	// still a lower bound.
	// ponytail: wall-clock nanoseconds, so a clock stepped backwards freezes
	// every report and clear for this source until wall time passes the old
	// high-water mark; only a sequence allocator shared across plugin processes
	// would avoid that.
	roundSeq := time.Now().UnixNano()
	agents, err := client.ListAgents(ctx)
	if err != nil {
		return err
	}
	if log == nil {
		log = io.Discard
	}
	// Measurement and diagnostic lines are emitted from parallel pane
	// refreshes; the lock keeps each JSON record whole on the shared writer.
	log = &lockedWriter{w: log}

	live := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		live[agent.PaneID] = struct{}{}
	}

	// errors.Join drops the nils that successful steps append.
	var errMu sync.Mutex
	var errs []error
	appendErr := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		errs = append(errs, err)
	}
	appendErr(sweepStaleMetadata(ctx, client, eventPaneID, live, log, roundSeq))

	var wg sync.WaitGroup
	slots := make(chan struct{}, refreshConcurrency)
	for _, agent := range agents {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() {
				<-slots
				wg.Done()
			}()
			// Only the labels ReadsProcesses names pay for herdr's process-info
			// subprocess; every other pane is classified and measured without it.
			// A failed read is round-local, so it becomes a diagnostic rather
			// than an error that kills the run, and FromAgent decides whether
			// the pane can still be classified: a provider using process ids as
			// a tier degrades to its others, but opencode cannot tell its
			// versions apart and waits a round.
			var processes []provider.Process
			var processErr error
			if provider.ReadsProcesses(agent.Agent) {
				processes, processErr = client.ProcessInfo(ctx, agent.PaneID)
				if processErr != nil {
					appendErr(writeMaintenanceDiagnostic(log, MaintenanceDiagnostic{Operation: "process_info", PaneID: agent.PaneID, Error: processErr.Error()}))
				}
			}
			kind, ok := provider.FromAgent(agent.Agent, processes, processErr)
			if !ok {
				return
			}
			// The providers map is the single authority on which kinds can be measured.
			// Gating on it rather than on a parallel predicate means a kind nobody
			// registered blanks its row instead of silently keeping a previous
			// session's value, which is the safe direction for an unreadable pane.
			p := providers[kind]
			if p == nil {
				if hasOccupancyToken(agent.Tokens) {
					if err := client.Clear(ctx, agent.PaneID, roundSeq); err != nil {
						appendErr(fmt.Errorf("clear superseded metadata for %s: %w", agent.PaneID, err))
					}
				}
				return
			}
			appendErr(refreshOne(ctx, client, agent.PaneID, kind, paneOf(agent, processes), log, p, roundSeq))
		}()
	}
	wg.Wait()
	// Every pane collects its own errors, and one the round's deadline cut
	// short need not have produced any. Reading the context directly keeps a
	// deadline-cut round from reporting success.
	if err := ctx.Err(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func sweepStaleMetadata(ctx context.Context, client Herdr, eventPaneID string, live map[string]struct{}, log io.Writer, seq int64) error {
	var stale []string
	var errs []error
	panes, err := client.ListPanes(ctx)
	if err != nil {
		errs = append(errs, writeMaintenanceDiagnostic(log, MaintenanceDiagnostic{Operation: "list_panes", Error: err.Error()}))
		// The event pane is still useful as a fallback when pane enumeration races.
		if eventPaneID != "" {
			stale = append(stale, eventPaneID)
		}
	}
	for _, pane := range panes {
		if hasOccupancyToken(pane.Tokens) {
			stale = append(stale, pane.PaneID)
		}
	}
	for _, paneID := range stale {
		if _, ok := live[paneID]; ok {
			continue
		}
		// A later event retries the sweep; measurement delivery owns exit status.
		if err := client.Clear(ctx, paneID, seq); err != nil {
			errs = append(errs, writeMaintenanceDiagnostic(log, MaintenanceDiagnostic{Operation: "clear_stale_metadata", PaneID: paneID, Error: err.Error()}))
		}
	}
	return errors.Join(errs...)
}

func hasOccupancyToken(tokens map[string]string) bool {
	// Clearing remains source-scoped even though Herdr returns a flat token map.
	_, hasDisplay := tokens["ctx"]
	_, hasRaw := tokens["ctx_raw"]
	return hasDisplay || hasRaw
}

func writeMeasurement(writer io.Writer, measurement Measurement) error {
	measurement.Type = "measurement"
	if err := json.NewEncoder(writer).Encode(measurement); err != nil {
		return fmt.Errorf("record measurement: %w", err)
	}
	return nil
}

// lockedWriter serializes writes so parallel pane refreshes cannot interleave
// JSON records; the encoder emits one Write per record.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func writeMaintenanceDiagnostic(writer io.Writer, diagnostic MaintenanceDiagnostic) error {
	diagnostic.Type = "maintenance"
	if err := json.NewEncoder(writer).Encode(diagnostic); err != nil {
		return fmt.Errorf("record maintenance diagnostic: %w", err)
	}
	return nil
}
