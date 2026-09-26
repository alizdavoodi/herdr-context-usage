package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencode"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type opencodeProvider struct{ db, dataHome string }

func (opencodeProvider) Kind() provider.Kind { return provider.OpenCode }

func (opencodeProvider) States() report.States { return opencode.SemanticStates() }

func (opencodeProvider) JustCompacted() error { return opencode.ErrJustCompacted }

// Resolve resolves in two stages. First, opencode.Resolve tries the agent
// session; if that fails, the store is opened and the title fallback runs.
// The resolution mode stays unresolved until one of them succeeds, so an
// open-store failure after a failed native resolution reports no mode. The
// store is opened before the fallback and closed after reading, so Cleanup is
// set only once the store is open.
func (p opencodeProvider) Resolve(ctx context.Context, pane provider.Pane) (provider.Resolved, error) {
	input := opencode.SessionInput{AgentSession: pane.AgentSession}
	native, nativeErr := opencode.Resolve(input)
	var mode provider.ResolutionMode
	if nativeErr == nil {
		mode = provider.ResolutionMode(native.Mode)
	}
	store, err := opencode.Open(ctx, p.db, p.dataHome)
	if err != nil {
		return provider.Resolved{Mode: mode}, err
	}
	resolved := provider.Resolved{Mode: mode, Cleanup: store.Close}
	if nativeErr != nil {
		fallback, err := store.ResolveTerminalTitle(ctx, pane.ForegroundCWD, pane.TerminalTitle)
		if err != nil {
			return resolved, err
		}
		resolved.Mode = provider.ResolutionMode(fallback.Mode)
		native = fallback
	}
	sessionID := native.ID
	resolved.Read = func(ctx context.Context) (provider.Reading, error) {
		value, err := store.ReadOccupancy(ctx, sessionID)
		return provider.Reading{Occupancy: value}, err
	}
	return resolved, nil
}
