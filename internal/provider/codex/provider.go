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

// Resolve reads no process list: codex's working-directory tier tells its own
// main thread apart by rollout metadata and writer locks, not by PID. Reading
// is file-backed, so Cleanup is nil.
func (p codexProvider) Resolve(_ context.Context, pane provider.Pane) (provider.Resolved, error) {
	resolved, err := resolve(sessionInput{
		CodexHome:     p.home,
		AgentSession:  pane.AgentSession,
		ForegroundCWD: pane.ForegroundCWD,
	})
	if err != nil {
		return provider.Resolved{}, err
	}
	path := resolved.Path
	return provider.Resolved{
		Mode: resolved.Mode,
		Read: func(context.Context) (provider.Reading, error) {
			value, err := readOccupancy(path)
			return provider.Reading{Occupancy: value}, err
		},
	}, nil
}
