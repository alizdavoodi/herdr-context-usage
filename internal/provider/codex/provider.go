package codex

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// New returns the codex provider. home is the value main read from
// CODEX_HOME, empty for the default under the user's home directory; it is
// resolved lazily, so a pane that reports a rollout path never needs it.
func New(home string) provider.Provider {
	return codexProvider{home: home}
}

type codexProvider struct{ home string }

// Measure reads no process list: codex's working-directory tier tells its own
// main thread apart by rollout metadata and writer locks, not by PID.
func (p codexProvider) Measure(_ context.Context, pane provider.Pane) (provider.Reading, error) {
	resolved, err := resolve(sessionInput{
		CodexHome:     p.home,
		AgentSession:  pane.AgentSession,
		ForegroundCWD: pane.ForegroundCWD,
	})
	if err != nil {
		return provider.Reading{}, err
	}
	value, err := readOccupancy(resolved.Path)
	return provider.Reading{Mode: resolved.Mode, Occupancy: value}, err
}
