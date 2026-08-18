package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencode2"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type opencode2Provider struct{}

func (opencode2Provider) Kind() provider.Kind { return provider.OpenCode2 }

func (opencode2Provider) States() report.States { return opencode2.SemanticStates() }

func (opencode2Provider) JustCompacted() error { return opencode2.ErrJustCompacted }

// Resolve opens the store and resolves the session from ForegroundCWD plus
// the TUI's terminal title. The store's own reading carries both an
// occupancy and a ModelID, so the Read closure forwards the ModelID to the
// measurement record. Cleanup closes the store after reading.
func (opencode2Provider) Resolve(ctx context.Context, agent herdr.Agent, _ []provider.Process, config Config) (Resolved, error) {
	store, err := opencode2.Open(ctx, config.OpenCode2DB)
	if err != nil {
		return Resolved{}, err
	}
	resolved, err := store.Resolve(ctx, agent.ForegroundCWD, agent.TerminalTitle)
	if err != nil {
		// Hand the open store back and let refreshOne close it, matching
		// opencode, whose two-stage resolution cannot close inline. One idiom
		// for a failed resolution is what a new provider copies correctly.
		return Resolved{Cleanup: store.Close}, err
	}
	mode := string(resolved.Mode)
	sessionID := resolved.ID
	return Resolved{
		Mode:    &mode,
		Cleanup: store.Close,
		Read: func(ctx context.Context) (Reading, error) {
			reading, err := store.ReadOccupancy(ctx, sessionID)
			return Reading{Occupancy: reading.Occupancy, ModelID: reading.ModelID}, err
		},
	}, nil
}
