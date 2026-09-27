package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"golang.org/x/sync/errgroup"
)

type Herdr interface {
	ListAgents(context.Context) ([]herdr.Agent, error)
	ListPanes(context.Context) ([]herdr.Pane, error)
	ProcessInfo(context.Context, string) ([]provider.Process, error)
	Report(context.Context, string, string, string) error
	Clear(context.Context, string) error
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

func Refresh(ctx context.Context, client Herdr, eventPaneID string, log io.Writer, config Config) error {
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

	var errMu sync.Mutex
	var errs []error
	appendErr := func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		errs = append(errs, err)
	}
	if err := sweepStaleMetadata(ctx, client, eventPaneID, live, log); err != nil {
		appendErr(err)
	}

	providers := registry(config)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(refreshConcurrency)
	for _, agent := range agents {
		g.Go(func() error {
			processes, processErr := client.ProcessInfo(gctx, agent.PaneID)
			if processErr != nil {
				// The process list matters in two ways: opencode classification
				// cannot happen without it, and every provider that uses
				// process IDs as a resolution tier degrades to its other
				// tiers. Either way the failure is round-local, so it becomes
				// a diagnostic rather than an error that kills the run.
				if err := writeMaintenanceDiagnostic(log, MaintenanceDiagnostic{Operation: "process_info", PaneID: agent.PaneID, Error: processErr.Error()}); err != nil {
					appendErr(err)
				}
				if provider.NeedsProcessList(agent.Agent) {
					return nil
				}
			}
			kind := provider.FromAgent(agent.Agent, processes)
			// The registry is the single authority on which kinds can be measured.
			// Gating on it rather than on a parallel predicate means a kind nobody
			// registered blanks its row instead of silently keeping a previous
			// session's value, which is the safe direction for an unreadable pane.
			p := providers[kind]
			if p == nil {
				if hasOccupancyToken(agent.Tokens) {
					if err := client.Clear(gctx, agent.PaneID); err != nil {
						appendErr(fmt.Errorf("clear superseded metadata for %s: %w", agent.PaneID, err))
					}
				}
				return nil
			}
			if err := refreshOne(gctx, client, agent.PaneID, kind, paneOf(agent, processes), log, p); err != nil {
				appendErr(err)
			}
			return nil
		})
	}
	// Every closure collects its own errors and returns nil, so Wait itself is
	// nil even when the round's deadline kills stragglers mid-flight. Reading
	// the parent context directly keeps a deadline-cut round from reporting
	// success.
	if err := g.Wait(); err != nil {
		errs = append(errs, err)
	}
	if err := ctx.Err(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func sweepStaleMetadata(ctx context.Context, client Herdr, eventPaneID string, live map[string]struct{}, log io.Writer) error {
	panes, err := client.ListPanes(ctx)
	if err != nil {
		var diagnosticErrs []error
		if writeErr := writeMaintenanceDiagnostic(log, MaintenanceDiagnostic{Operation: "list_panes", Error: err.Error()}); writeErr != nil {
			diagnosticErrs = append(diagnosticErrs, writeErr)
		}
		// The event pane is still useful as a fallback when pane enumeration races.
		if eventPaneID != "" {
			if _, ok := live[eventPaneID]; !ok {
				if clearErr := client.Clear(ctx, eventPaneID); clearErr != nil {
					if writeErr := writeMaintenanceDiagnostic(log, MaintenanceDiagnostic{Operation: "clear_stale_metadata", PaneID: eventPaneID, Error: clearErr.Error()}); writeErr != nil {
						diagnosticErrs = append(diagnosticErrs, writeErr)
					}
				}
			}
		}
		return errors.Join(diagnosticErrs...)
	}

	var diagnosticErrs []error
	for _, pane := range panes {
		if _, ok := live[pane.PaneID]; ok || !hasOccupancyToken(pane.Tokens) {
			continue
		}
		if clearErr := client.Clear(ctx, pane.PaneID); clearErr != nil {
			// A later event retries the sweep; measurement delivery owns exit status.
			if writeErr := writeMaintenanceDiagnostic(log, MaintenanceDiagnostic{Operation: "clear_stale_metadata", PaneID: pane.PaneID, Error: clearErr.Error()}); writeErr != nil {
				diagnosticErrs = append(diagnosticErrs, writeErr)
			}
			continue
		}
	}
	return errors.Join(diagnosticErrs...)
}

func hasOccupancyToken(tokens map[string]string) bool {
	// Clearing remains source-scoped even though Herdr returns a flat token map.
	_, hasDisplay := tokens["ctx"]
	_, hasRaw := tokens["ctx_raw"]
	return hasDisplay || hasRaw
}

// handleUnknown is the one place a failed reading becomes a decision about the
// pane, for every provider. Each provider brings its own superseded answers and
// its own half-built record; only the classification is shared. refreshOne
// fills the record in because how much a failed reading observed before giving
// up varies per provider, and threading each of those fields through here as a
// parameter is how this signature grew unreadable once already.
func handleUnknown(ctx context.Context, client Herdr, log io.Writer, superseded []error, measurement Measurement, err error) error {
	measurement.Error = err.Error()
	measurementErr := writeMeasurement(log, measurement)
	if provider.IsSuperseded(err, superseded) {
		return errors.Join(measurementErr, client.Clear(ctx, measurement.PaneID))
	}
	// Anything undeclared leaves the displayed value alone. An answer such as
	// codex's ErrFailedTurn leaves the previous reading standing as a valid
	// lower bound, a transient one is a failure to read, and neither is
	// evidence that the value belongs to a superseded session.
	return measurementErr
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
