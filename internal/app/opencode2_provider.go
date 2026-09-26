package app

import (
	"context"
	"errors"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencode2"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type opencode2Provider struct{ db, dataHome string }

func (opencode2Provider) Kind() provider.Kind { return provider.OpenCode2 }

func (opencode2Provider) States() report.States { return opencode2.SemanticStates() }

func (opencode2Provider) JustCompacted() error { return opencode2.ErrJustCompacted }

// Resolve opens the store and resolves the session from ForegroundCWD plus
// the TUI's terminal title. The store's own reading carries both an
// occupancy and a ModelID, so the Read closure forwards the ModelID to the
// measurement record. Cleanup closes the store after reading.
func (p opencode2Provider) Resolve(ctx context.Context, pane provider.Pane) (provider.Resolved, error) {
	store, err := opencode2.Open(ctx, p.db, p.dataHome)
	if err != nil {
		return provider.Resolved{}, err
	}
	resolved, err := store.Resolve(ctx, pane.ForegroundCWD, pane.TerminalTitle)
	if err != nil {
		// Hand the open store back and let refreshOne close it, matching
		// opencode, whose two-stage resolution cannot close inline. One idiom
		// for a failed resolution is what a new provider copies correctly.
		return provider.Resolved{Cleanup: store.Close}, attributeMiniInterface(err, pane.Processes)
	}
	sessionID := resolved.ID
	return provider.Resolved{
		Mode:    provider.ResolutionMode(resolved.Mode),
		Cleanup: store.Close,
		Read: func(ctx context.Context) (provider.Reading, error) {
			reading, err := store.ReadOccupancy(ctx, sessionID)
			return provider.Reading{Occupancy: reading.Occupancy, ModelID: reading.ModelID}, err
		},
	}, nil
}

// attributeMiniInterface names the mini interface as the reason a pane reports
// no opencode title, when the pane's own opencode2 process says that is what it
// is running.
//
// Only an unavailable title is reattributed, which is what makes this
// attribution rather than parsing. A bare "OpenCode" title, a plugin route, a
// well-formed title matching no session and a store failure each already carry
// their own reason and keep it. The mini answer wraps the one it replaces, so
// the row blanks exactly as before and SemanticStates needs no new entry.
func attributeMiniInterface(err error, processes []provider.Process) error {
	if !errors.Is(err, opencodeshared.ErrTitleUnavailable) {
		return err
	}
	for _, process := range processes {
		if process.Kind() == provider.OpenCode2 && opencode2.RunsMiniInterface(process.Argv) {
			return opencode2.ErrMiniInterface
		}
	}
	return err
}
