package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	occupancyformat "github.com/alizdavoodi/herdr-context-usage/internal/format"
	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/claude"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/codex"
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
	ClaudeHome string
	CodexHome  string
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
	Error          string        `json:"error,omitempty"`
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
		if !kind.Implemented() {
			if hasOccupancyToken(agent.Tokens) {
				if err := client.Clear(ctx, agent.PaneID); err != nil {
					errs = append(errs, fmt.Errorf("clear superseded metadata for %s: %w", agent.PaneID, err))
				}
			}
			if processErr != nil {
				errs = append(errs, processErr)
			}
			continue
		}
		if processErr != nil {
			errs = append(errs, processErr)
			continue
		}
		var refreshErr error
		switch kind {
		case provider.Claude:
			refreshErr = refreshClaude(ctx, client, agent, processes, log, config)
		case provider.Codex:
			refreshErr = refreshCodex(ctx, client, agent, log, config)
		}
		if refreshErr != nil {
			errs = append(errs, refreshErr)
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

func refreshClaude(ctx context.Context, client Herdr, agent herdr.Agent, processes []provider.Process, log io.Writer, config Config) error {
	input := claude.SessionInput{
		ClaudeHome:    config.ClaudeHome,
		ForegroundCWD: agent.ForegroundCWD,
		ProcessIDs:    claudeProcessIDs(processes),
	}
	if agent.AgentSession != nil {
		input.AgentSession = &claude.AgentSession{
			Source: agent.AgentSession.Source,
			Agent:  agent.AgentSession.Agent,
			Kind:   agent.AgentSession.Kind,
			Value:  agent.AgentSession.Value,
		}
	}

	resolved, err := claude.Resolve(input)
	if err != nil {
		measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Claude, ContextState: ContextUnknown, Error: err.Error()}
		measurementErr := writeMeasurement(log, measurement)
		if report.SupersedesCurrentValue(err, claude.SemanticStates()) {
			return errors.Join(measurementErr, client.Clear(ctx, agent.PaneID))
		}
		return measurementErr
	}
	mode := string(resolved.Mode)
	value, err := claude.ReadOccupancy(resolved.Path)
	if err != nil {
		state := ContextUnknown
		if errors.Is(err, claude.ErrJustCompacted) {
			state = ContextJustCompacted
		}
		measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Claude, ContextState: state, ResolutionMode: &mode, Error: err.Error()}
		measurementErr := writeMeasurement(log, measurement)
		if report.SupersedesCurrentValue(err, claude.SemanticStates()) {
			return errors.Join(measurementErr, client.Clear(ctx, agent.PaneID))
		}
		return measurementErr
	}

	measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Claude, ContextState: ContextMeasured, Occupancy: &value, ResolutionMode: &mode}
	reportErr := client.Report(ctx, agent.PaneID, occupancyformat.Occupancy(value), strconv.FormatInt(value, 10))
	measurementErr := writeMeasurement(log, measurement)
	return errors.Join(measurementErr, reportErr)
}

func refreshCodex(ctx context.Context, client Herdr, agent herdr.Agent, log io.Writer, config Config) error {
	input := codex.SessionInput{CodexHome: config.CodexHome, ForegroundCWD: agent.ForegroundCWD}
	if agent.AgentSession != nil {
		input.AgentSession = &codex.AgentSession{
			Source: agent.AgentSession.Source,
			Agent:  agent.AgentSession.Agent,
			Kind:   agent.AgentSession.Kind,
			Value:  agent.AgentSession.Value,
		}
	}

	resolved, err := codex.Resolve(input)
	if err != nil {
		measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Codex, ContextState: ContextUnknown, Error: err.Error()}
		measurementErr := writeMeasurement(log, measurement)
		if report.SupersedesCurrentValue(err, codex.SemanticStates()) {
			return errors.Join(measurementErr, client.Clear(ctx, agent.PaneID))
		}
		return measurementErr
	}

	mode := string(resolved.Mode)
	value, err := codex.ReadOccupancy(resolved.Path)
	if err != nil {
		state := ContextUnknown
		if errors.Is(err, codex.ErrJustCompacted) {
			state = ContextJustCompacted
		}
		measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Codex, ContextState: state, ResolutionMode: &mode, Error: err.Error()}
		measurementErr := writeMeasurement(log, measurement)
		if report.SupersedesCurrentValue(err, codex.SemanticStates()) {
			return errors.Join(measurementErr, client.Clear(ctx, agent.PaneID))
		}
		return measurementErr
	}

	measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Codex, ContextState: ContextMeasured, Occupancy: &value, ResolutionMode: &mode}
	reportErr := client.Report(ctx, agent.PaneID, occupancyformat.Occupancy(value), strconv.FormatInt(value, 10))
	measurementErr := writeMeasurement(log, measurement)
	return errors.Join(measurementErr, reportErr)
}

func claudeProcessIDs(processes []provider.Process) []int {
	return provider.ProcessIDs(processes, provider.Claude)
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
