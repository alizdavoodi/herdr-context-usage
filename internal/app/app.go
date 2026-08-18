package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
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
	// The pi home is per-provider rather than shared with omp even though both
	// agents honour one override variable: a single field would point a pi pane
	// at omp's store on any machine running both.
	PiHome string
}

type ContextState string

const (
	ContextMeasured      ContextState = "measured"
	ContextUnknown       ContextState = "unknown"
	ContextJustCompacted ContextState = "just_compacted"
)

type Measurement struct {
	Type           string        `json:"type"`
	PaneID         string        `json:"pane_id"`
	Provider       provider.Kind `json:"provider"`
	ContextState   ContextState  `json:"context_state"`
	Occupancy      *int64        `json:"occupancy"`
	ResolutionMode *string       `json:"resolution_mode"`
	ModelID        string        `json:"model_id,omitempty"`
	// The version is not a compatibility gate — pi and omp stamp the same one
	// while carrying different field sets — so it is carried into the output
	// instead: that is what makes a format change in a floating dependency
	// visible rather than leaving it to surface only as a blank row. omitempty
	// keeps the records of providers whose stores carry no version unchanged.
	TranscriptVersion *int   `json:"transcript_version,omitempty"`
	Error             string `json:"error,omitempty"`
}

type MaintenanceDiagnostic struct {
	Type      string `json:"type"`
	Operation string `json:"operation"`
	PaneID    string `json:"pane_id,omitempty"`
	Error     string `json:"error"`
}

func Refresh(ctx context.Context, client Herdr, eventPaneID string, log io.Writer, config Config) error {
	agents, err := client.ListAgents(ctx)
	if err != nil {
		return err
	}
	if log == nil {
		log = io.Discard
	}

	live := make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		live[agent.PaneID] = struct{}{}
	}

	var errs []error
	if err := sweepStaleMetadata(ctx, client, eventPaneID, live, log); err != nil {
		errs = append(errs, err)
	}
	for _, agent := range agents {
		processes, processErr := client.ProcessInfo(ctx, agent.PaneID)
		kind := provider.FromAgent(agent.Agent, provider.ProcessList{Entries: processes, Available: processErr == nil})
		if processErr != nil {
			errs = append(errs, processErr)
			continue
		}
		// The registry is the single authority on which kinds can be measured.
		// Gating on it rather than on a parallel predicate means a kind nobody
		// registered blanks its row instead of silently keeping a previous
		// session's value, which is the safe direction for an unreadable pane.
		p := providerFor(kind)
		if p == nil {
			if hasOccupancyToken(agent.Tokens) {
				if err := client.Clear(ctx, agent.PaneID); err != nil {
					errs = append(errs, fmt.Errorf("clear superseded metadata for %s: %w", agent.PaneID, err))
				}
			}
			continue
		}
		if err := refreshOne(ctx, client, agent, processes, log, config, p); err != nil {
			errs = append(errs, err)
		}
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
// pane, for every provider. Each provider brings its own states and its own
// half-built record; only the classification is shared. refreshOne fills the
// record in because how much a failed reading observed before giving up varies
// per provider, and threading each of those fields through here as a parameter
// is how this signature grew unreadable once already.
func handleUnknown(ctx context.Context, client Herdr, log io.Writer, states report.States, measurement Measurement, err error) error {
	measurement.Error = err.Error()
	measurementErr := writeMeasurement(log, measurement)
	if report.Classify(err, states) == report.Superseded {
		return errors.Join(measurementErr, client.Clear(ctx, measurement.PaneID))
	}
	// Stale-low and transient answers both leave the displayed value alone: one
	// is a valid lower bound, the other is a failure to read, and neither is
	// evidence that the value belongs to a superseded session.
	return measurementErr
}

// contextState maps a provider's own just-compacted answer onto the state herdr
// sees. Each provider detects a compaction against its own store, in shapes
// that have nothing in common; reporting it is what they share.
func contextState(err, justCompacted error) ContextState {
	if errors.Is(err, justCompacted) {
		return ContextJustCompacted
	}
	return ContextUnknown
}

func writeMeasurement(writer io.Writer, measurement Measurement) error {
	measurement.Type = "measurement"
	if err := json.NewEncoder(writer).Encode(measurement); err != nil {
		return fmt.Errorf("record measurement: %w", err)
	}
	return nil
}

func writeMaintenanceDiagnostic(writer io.Writer, diagnostic MaintenanceDiagnostic) error {
	diagnostic.Type = "maintenance"
	if err := json.NewEncoder(writer).Encode(diagnostic); err != nil {
		return fmt.Errorf("record maintenance diagnostic: %w", err)
	}
	return nil
}
