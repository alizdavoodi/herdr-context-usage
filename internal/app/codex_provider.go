package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/codex"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type codexProvider struct{}

func (codexProvider) Kind() provider.Kind { return provider.Codex }

func (codexProvider) States() report.States { return codex.SemanticStates() }

func (codexProvider) JustCompacted() error { return codex.ErrJustCompacted }

// Resolve builds the codex session input — CodexHome and ForegroundCWD from
// config/agent — then resolves the session path. Codex uses no process list.
// Reading is file-backed, so Cleanup is nil.
func (codexProvider) Resolve(_ context.Context, agent herdr.Agent, _ []provider.Process, config Config) (Resolved, error) {
	input := codex.SessionInput{CodexHome: config.CodexHome, AgentSession: agent.AgentSession, ForegroundCWD: agent.ForegroundCWD}
	resolved, err := codex.Resolve(input)
	if err != nil {
		return Resolved{}, err
	}
	mode := string(resolved.Mode)
	path := resolved.Path
	return Resolved{
		Mode: &mode,
		Read: func(_ context.Context) (Reading, error) {
			value, err := codex.ReadOccupancy(path)
			return Reading{Occupancy: value}, err
		},
	}, nil
}
