package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/pi"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type piProvider struct{}

func (piProvider) Kind() provider.Kind { return provider.Pi }

func (piProvider) States() report.States { return pi.SemanticStates() }

func (piProvider) JustCompacted() error { return pi.ErrJustCompacted }

// Resolve builds the pi session input from PiHome and herdr's reported agent
// session, and nothing else. Deliberately absent, unlike claude: ForegroundCWD
// and the pane's process list. pi files an agent session under the directory it
// started in, so a pane that has since moved would resolve to an unrelated
// transcript or to none, and the transcript does not mark a sub-agent session,
// so a directory-scoped match has nothing to exclude one with. Wiring either
// stage in by analogy with claude would trade a blank row for a wrong number.
// Reading is file-backed, so Cleanup is nil.
func (piProvider) Resolve(_ context.Context, agent herdr.Agent, _ []provider.Process, config Config) (Resolved, error) {
	input := pi.SessionInput{PiHome: config.PiHome}
	if agent.AgentSession != nil {
		input.AgentSession = &pi.AgentSession{
			Source: agent.AgentSession.Source,
			Agent:  agent.AgentSession.Agent,
			Kind:   agent.AgentSession.Kind,
			Value:  agent.AgentSession.Value,
		}
	}
	resolved, err := pi.Resolve(input)
	if err != nil {
		return Resolved{}, err
	}
	mode := string(resolved.Mode)
	path := resolved.Path
	return Resolved{
		Mode: &mode,
		Read: func(_ context.Context) (Reading, error) {
			// The version comes back even when the error does: a just-compacted
			// transcript was read right up to the compaction record, and that
			// row still has to name the version it was read from.
			reading, err := pi.ReadOccupancy(path)
			return Reading{Occupancy: reading.Occupancy, TranscriptVersion: reading.Version}, err
		},
	}, nil
}
