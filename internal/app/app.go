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
}

type ContextState string

const (
	ContextMeasured      ContextState = "measured"
	ContextUnknown       ContextState = "unknown"
	ContextJustCompacted ContextState = "just_compacted"
)

type Measurement struct {
	PaneID         string                 `json:"pane_id"`
	Provider       provider.Kind          `json:"provider"`
	ContextState   ContextState           `json:"context_state"`
	Occupancy      *int64                 `json:"occupancy"`
	ResolutionMode *claude.ResolutionMode `json:"resolution_mode"`
	Error          string                 `json:"error,omitempty"`
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
	panes, err := client.ListPanes(ctx)
	if err != nil {
		errs = append(errs, err)
		// The event pane is still useful as a fallback when pane enumeration races.
		if eventPaneID != "" {
			if _, ok := live[eventPaneID]; !ok {
				if clearErr := client.Clear(ctx, eventPaneID); clearErr != nil {
					errs = append(errs, clearErr)
				}
			}
		}
	} else {
		for _, pane := range panes {
			if _, ok := live[pane.PaneID]; ok || !hasOccupancyToken(pane.Tokens) {
				continue
			}
			if err := client.Clear(ctx, pane.PaneID); err != nil {
				errs = append(errs, err)
			}
		}
	}

	for _, agent := range agents {
		processes, err := client.ProcessInfo(ctx, agent.PaneID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		kind := provider.FromProcesses(processes)
		if !kind.Implemented() {
			continue
		}
		if err := refreshClaude(ctx, client, agent, processes, log, config); err != nil {
			errs = append(errs, err)
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
		return writeMeasurement(log, measurement)
	}
	mode := resolved.Mode
	value, err := claude.ReadOccupancy(resolved.Path)
	if err != nil {
		state := ContextUnknown
		if errors.Is(err, claude.ErrJustCompacted) {
			state = ContextJustCompacted
		}
		measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Claude, ContextState: state, ResolutionMode: &mode, Error: err.Error()}
		return writeMeasurement(log, measurement)
	}

	measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Claude, ContextState: ContextMeasured, Occupancy: &value, ResolutionMode: &mode}
	if err := writeMeasurement(log, measurement); err != nil {
		return err
	}
	if !report.ShouldWrite(value) {
		return nil
	}
	return client.Report(ctx, agent.PaneID, occupancyformat.Occupancy(value), strconv.FormatInt(value, 10))
}

func claudeProcessIDs(processes []provider.Process) []int {
	ids := make([]int, 0, 1)
	for _, process := range processes {
		if provider.FromProcesses([]provider.Process{process}) == provider.Claude {
			ids = append(ids, process.PID)
		}
	}
	return ids
}

func writeMeasurement(writer io.Writer, measurement Measurement) error {
	if err := json.NewEncoder(writer).Encode(measurement); err != nil {
		return fmt.Errorf("record measurement: %w", err)
	}
	return nil
}
