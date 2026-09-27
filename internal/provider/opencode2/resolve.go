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

// Measure correlates by the pane's directory and the TUI's terminal title,
// because herdr reports no agent session for an opencode2 pane: neither opencode
// version sends herdr a session id, as the README records under "When an
// opencode row never fills in".
func (p opencode2Provider) Measure(ctx context.Context, pane provider.Pane) (provider.Reading, error) {
	s, err := open(ctx, p.db, p.dataHome)
	if err != nil {
		return provider.Reading{}, err
	}
	defer s.Close()
	id, err := s.resolveTerminalTitle(ctx, pane.ForegroundCWD, pane.TerminalTitle)
	if err != nil {
		return provider.Reading{}, attributeMiniInterface(err, pane.Processes)
	}
	reading, err := s.readOccupancy(ctx, id)
	reading.Mode = provider.ResolutionTerminalTitle
	return reading, err
}
