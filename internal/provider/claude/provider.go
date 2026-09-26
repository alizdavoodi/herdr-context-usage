package claude

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// New returns the claude provider. home is the value main read from
// CLAUDE_CONFIG_DIR, empty for the default under the user's home directory; it
// is resolved lazily, so a pane that reports a transcript path never needs it.
func New(home string) provider.Provider {
	return claudeProvider{home: home}
}

type claudeProvider struct{ home string }

func (claudeProvider) Kind() provider.Kind { return provider.Claude }

func (claudeProvider) JustCompacted() error { return ErrJustCompacted }

// Resolve narrows the pane's process list to claude's own process ids, which
// is what the process-registry tier matches on. Reading is file-backed, so
// Cleanup is nil.
func (p claudeProvider) Resolve(_ context.Context, pane provider.Pane) (provider.Resolved, error) {
	resolved, err := resolve(sessionInput{
		ClaudeHome:    p.home,
		AgentSession:  pane.AgentSession,
		ForegroundCWD: pane.ForegroundCWD,
		ProcessIDs:    provider.ProcessIDs(pane.Processes, provider.Claude),
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
