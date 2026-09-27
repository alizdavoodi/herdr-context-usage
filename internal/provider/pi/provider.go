package pi

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// New returns the pi provider. home is the value main read from
// PI_CODING_AGENT_DIR, empty for the default under the user's home directory;
// it is resolved lazily, so a pane that reports a transcript path never needs
// it.
func New(home string) provider.Provider {
	return piProvider{home: home}
}

type piProvider struct{ home string }

// Measure reads only herdr's reported agent session from the pane; resolve says
// why.
func (p piProvider) Measure(_ context.Context, pane provider.Pane) (provider.Reading, error) {
	path, err := resolve(p.home, pane.AgentSession)
	if err != nil {
		return provider.Reading{}, err
	}
	reading, err := readOccupancy(path)
	// Always agent_session, because resolution has one source: herdr's reported
	// agent session is the whole of the input, so resolve's branches differ only
	// in how they read one value, and a second mode would name a stage that does
	// not exist.
	reading.Mode = provider.ResolutionAgentSession
	return reading, err
}
