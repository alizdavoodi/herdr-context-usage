package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/claude"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type claudeProvider struct{ home string }

func (claudeProvider) Kind() provider.Kind { return provider.Claude }

func (claudeProvider) States() report.States { return claude.SemanticStates() }

func (claudeProvider) JustCompacted() error { return claude.ErrJustCompacted }

// Resolve builds the claude session input — the configured home and the pane's
// ForegroundCWD, ProcessIDs derived from the pane's process list — then
// resolves the session path. Reading is file-backed, so Cleanup is nil.
func (p claudeProvider) Resolve(_ context.Context, pane provider.Pane) (provider.Resolved, error) {
	input := claude.SessionInput{
		ClaudeHome:    p.home,
		AgentSession:  pane.AgentSession,
		ForegroundCWD: pane.ForegroundCWD,
		ProcessIDs:    provider.ProcessIDs(pane.Processes, provider.Claude),
	}
	resolved, err := claude.Resolve(input)
	if err != nil {
		return provider.Resolved{}, err
	}
	path := resolved.Path
	return provider.Resolved{
		Mode: provider.ResolutionMode(resolved.Mode),
		Read: func(_ context.Context) (provider.Reading, error) {
			value, err := claude.ReadOccupancy(path)
			return provider.Reading{Occupancy: value}, err
		},
	}, nil
}
