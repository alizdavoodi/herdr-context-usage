package opencode

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// New returns the opencode v1 provider. explicitDB and dataHome are the values
// main read from OPENCODE_DB and XDG_DATA_HOME, empty for opencode's own default
// under the user's home. Both are resolved per refresh rather than here, so a
// host with no home blanks the pane instead of failing construction.
func New(explicitDB, dataHome string) provider.Provider {
	return opencodeProvider{db: explicitDB, dataHome: dataHome}
}

type opencodeProvider struct{ db, dataHome string }

func (opencodeProvider) Kind() provider.Kind { return provider.OpenCode }

func (opencodeProvider) JustCompacted() error { return ErrJustCompacted }

// Resolve tries the session id herdr reported, then the pane's terminal title.
// Only an id is consulted: opencode keeps its sessions in a database rather
// than a file per session, so a reported path names nothing this provider can
// read and is refused.
//
// The store opens between the two stages because the title fallback and the
// reading both query it. The mode stays unresolved until a stage succeeds, so
// a store that fails to open after a refused id records no mode, while one
// after an accepted id keeps agent_session. Cleanup is set only once the store
// is open.
func (p opencodeProvider) Resolve(ctx context.Context, pane provider.Pane) (provider.Resolved, error) {
	id, reported := pane.AgentSession.ID()
	var mode provider.ResolutionMode
	if reported {
		mode = provider.ResolutionAgentSession
	}
	s, err := open(ctx, p.db, p.dataHome)
	if err != nil {
		return provider.Resolved{Mode: mode}, err
	}
	if !reported {
		id, err = s.resolveTerminalTitle(ctx, pane.ForegroundCWD, pane.TerminalTitle)
		if err != nil {
			return provider.Resolved{Cleanup: s.Close}, err
		}
		mode = provider.ResolutionTerminalTitle
	}
	return provider.Resolved{
		Mode:    mode,
		Cleanup: s.Close,
		Read: func(ctx context.Context) (provider.Reading, error) {
			value, err := s.readOccupancy(ctx, id)
			return provider.Reading{Occupancy: value}, err
		},
	}, nil
}
