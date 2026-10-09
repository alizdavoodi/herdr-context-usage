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

// Measure narrows the pane's process list to claude's own process ids, which
// is what the process-registry tier matches on.
func (p claudeProvider) Measure(_ context.Context, pane provider.Pane) (provider.Reading, error) {
	resolved, err := resolve(sessionInput{
		ClaudeHome:    p.home,
		AgentSession:  pane.AgentSession,
		ForegroundCWD: pane.ForegroundCWD,
		ProcessIDs:    processIDs(pane.Processes),
	})
	if err != nil {
		return provider.Reading{}, err
	}
	value, err := readOccupancy(resolved.Path)
	return provider.Reading{Mode: resolved.Mode, Occupancy: value}, err
}

func processIDs(processes []provider.Process) []int {
	ids := make([]int, 0, 1)
	for _, process := range processes {
		if process.Kind() == provider.Claude {
			ids = append(ids, process.PID)
		}
	}
	return ids
}
