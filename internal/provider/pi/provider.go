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

// Resolve reads only herdr's reported agent session from the pane; sessionInput
// says why. Reading is file-backed, so Cleanup is nil.
func (p piProvider) Resolve(_ context.Context, pane provider.Pane) (provider.Resolved, error) {
	resolved, err := resolve(sessionInput{PiHome: p.home, AgentSession: pane.AgentSession})
	if err != nil {
		return provider.Resolved{}, err
	}
	path := resolved.Path
	return provider.Resolved{
		Mode: resolved.Mode,
		Read: func(context.Context) (provider.Reading, error) { return readOccupancy(path) },
	}, nil
}
