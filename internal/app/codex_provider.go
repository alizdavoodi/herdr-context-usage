package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/codex"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type codexProvider struct{ home string }

func (codexProvider) Kind() provider.Kind { return provider.Codex }

func (codexProvider) States() report.States { return codex.SemanticStates() }

func (codexProvider) JustCompacted() error { return codex.ErrJustCompacted }

// Resolve builds the codex session input — the configured home and the pane's
// ForegroundCWD — then resolves the session path. Codex uses no process list.
// Reading is file-backed, so Cleanup is nil.
func (p codexProvider) Resolve(_ context.Context, pane provider.Pane) (provider.Resolved, error) {
	input := codex.SessionInput{CodexHome: p.home, AgentSession: pane.AgentSession, ForegroundCWD: pane.ForegroundCWD}
	resolved, err := codex.Resolve(input)
	if err != nil {
		return provider.Resolved{}, err
	}
	path := resolved.Path
	return provider.Resolved{
		Mode: provider.ResolutionMode(resolved.Mode),
		Read: func(_ context.Context) (provider.Reading, error) {
			value, err := codex.ReadOccupancy(path)
			return provider.Reading{Occupancy: value}, err
		},
	}, nil
}
