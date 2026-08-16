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
	ProcessInfo(context.Context, string) ([]provider.Process, error)
	Report(context.Context, string, string, string) error
	Clear(context.Context, string) error
}

type Measurement struct {
	PaneID         string                 `json:"pane_id"`
	Provider       provider.Kind          `json:"provider"`
	Occupancy      *int64                 `json:"occupancy"`
	ResolutionMode *claude.ResolutionMode `json:"resolution_mode"`
	Error          string                 `json:"error,omitempty"`
}

func Refresh(ctx context.Context, client Herdr, eventPaneID string, log io.Writer) error {
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
	if eventPaneID != "" {
		if _, ok := live[eventPaneID]; !ok {
			if err := client.Clear(ctx, eventPaneID); err != nil {
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
		if err := refreshClaude(ctx, client, agent, processes, log); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func refreshClaude(ctx context.Context, client Herdr, agent herdr.Agent, processes []provider.Process, log io.Writer) error {
	input := claude.SessionInput{
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
		measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Claude, Error: err.Error()}
		return errors.Join(writeMeasurement(log, measurement), client.Clear(ctx, agent.PaneID))
	}
	mode := resolved.Mode
	value, err := claude.ReadOccupancy(resolved.Path)
	if err != nil {
		measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Claude, ResolutionMode: &mode, Error: err.Error()}
		return errors.Join(writeMeasurement(log, measurement), client.Clear(ctx, agent.PaneID))
	}

	measurement := Measurement{PaneID: agent.PaneID, Provider: provider.Claude, Occupancy: &value, ResolutionMode: &mode}
	if err := writeMeasurement(log, measurement); err != nil {
		return err
	}
	if !report.ShouldWrite(value, agent.Tokens) {
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
