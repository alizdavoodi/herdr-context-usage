package opencode2

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// New returns the opencode2 provider. explicitDB and dataHome are the values
// main read from OPENCODE2_DB and XDG_DATA_HOME, empty for opencode's own
// default under the user's home. Both are resolved per refresh rather than
// here, so a host with no home blanks the pane instead of failing construction.
func New(explicitDB, dataHome string) provider.Provider {
	return opencode2Provider{db: explicitDB, dataHome: dataHome}
}

type opencode2Provider struct{ db, dataHome string }

// Resolve correlates by the pane's directory and the TUI's terminal title,
// because herdr reports no agent session for an opencode2 pane: neither opencode
// version sends herdr a session id, as the README records under "When an
// opencode row never fills in".
func (p opencode2Provider) Resolve(ctx context.Context, pane provider.Pane) (provider.Resolved, error) {
	s, err := open(ctx, p.db, p.dataHome)
	if err != nil {
		return provider.Resolved{}, err
	}
	id, err := s.resolveTerminalTitle(ctx, pane.ForegroundCWD, pane.TerminalTitle)
	if err != nil {
		// Hand the open store back and let the skeleton close it, matching
		// opencode, whose two-stage resolution cannot close inline. One idiom
		// for a failed resolution is what a new provider copies correctly.
		return provider.Resolved{Cleanup: s.Close}, attributeMiniInterface(err, pane.Processes)
	}
	return provider.Resolved{
		Mode:    provider.ResolutionTerminalTitle,
		Cleanup: s.Close,
		Read: func(ctx context.Context) (provider.Reading, error) {
			return s.readOccupancy(ctx, id)
		},
	}, nil
}
