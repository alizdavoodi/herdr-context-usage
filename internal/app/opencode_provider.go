package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencode"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type opencodeProvider struct{}

func (opencodeProvider) Kind() provider.Kind { return provider.OpenCode }

func (opencodeProvider) States() report.States { return opencode.SemanticStates() }

func (opencodeProvider) JustCompacted() error { return opencode.ErrJustCompacted }

// Resolve resolves in two stages. First, opencode.Resolve tries the agent
// session; if that fails, the store is opened and the title fallback runs.
// The resolution mode is nil until one of them succeeds, so an open-store
// failure after a failed native resolution reports mode as nil. The store
// is opened before the fallback and closed after reading, so Cleanup is set
// only once the store is open.
func (opencodeProvider) Resolve(ctx context.Context, agent herdr.Agent, _ []provider.Process, config Config) (Resolved, error) {
	input := opencode.SessionInput{AgentSession: agent.AgentSession}
	native, nativeErr := opencode.Resolve(input)
	var mode *string
	if nativeErr == nil {
		resolvedMode := string(native.Mode)
		mode = &resolvedMode
	}
	store, err := opencode.Open(ctx, config.OpenCodeDB, config.OpenCodeDataHome)
	if err != nil {
		return Resolved{Mode: mode}, err
	}
	resolved := Resolved{Mode: mode, Cleanup: store.Close}
	if nativeErr != nil {
		fallback, err := store.ResolveTerminalTitle(ctx, agent.ForegroundCWD, agent.TerminalTitle)
		if err != nil {
			return resolved, err
		}
		fallbackMode := string(fallback.Mode)
		resolved.Mode = &fallbackMode
		native = fallback
	}
	sessionID := native.ID
	resolved.Read = func(ctx context.Context) (Reading, error) {
		value, err := store.ReadOccupancy(ctx, sessionID)
		return Reading{Occupancy: value}, err
	}
	return resolved, nil
}
