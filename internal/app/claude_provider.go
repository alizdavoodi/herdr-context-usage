package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/claude"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type claudeProvider struct{}

func (claudeProvider) Kind() provider.Kind { return provider.Claude }

func (claudeProvider) States() report.States { return claude.SemanticStates() }

func (claudeProvider) JustCompacted() error { return claude.ErrJustCompacted }

// Resolve builds the claude session input — ClaudeHome and ForegroundCWD from
// config/agent, ProcessIDs derived from the pane's process list — then resolves
// the session path. Reading is file-backed, so Cleanup is nil.
func (claudeProvider) Resolve(_ context.Context, agent herdr.Agent, processes []provider.Process, config Config) (Resolved, error) {
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
		return Resolved{}, err
	}
	mode := string(resolved.Mode)
	path := resolved.Path
	return Resolved{
		Mode: &mode,
		Read: func(_ context.Context) (int64, string, error) {
			value, err := claude.ReadOccupancy(path)
			return value, "", err
		},
	}, nil
}

func claudeProcessIDs(processes []provider.Process) []int {
	return provider.ProcessIDs(processes, provider.Claude)
}
