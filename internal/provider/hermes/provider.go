package hermes

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

var ErrNoSession = errors.New("hermes agent session cannot be resolved")

// New returns the hermes provider. home is the value main read from
// HERMES_HOME, empty for the default under the user's home directory; it is
// resolved per refresh rather than here, so a host with no home blanks the pane
// instead of failing construction.
func New(home string) provider.Provider {
	return hermesProvider{home: home}
}

type hermesProvider struct{ home string }

// Measure reads only herdr's reported agent session id, and nothing else the
// pane offers. There is no working-directory, title, process or recency
// fallback, and each would be wrong here rather than merely weak: a desktop
// session records no directory at all, a delegated sub-agent runs in its
// parent's directory, and recency is banned by ADR-0004. The main-thread
// invariant needs no filter either, because Herdr's hermes integration reports
// only from interactive platforms, so a sub-agent never reports a session.
//
// The integration always reports an id, the primary key of state.db's sessions
// table, so a path names nothing this store can read and is refused. Both
// refusals come before the home or the store is touched: they are answers about
// the pane, and a failing store must not mask them.
func (p hermesProvider) Measure(ctx context.Context, pane provider.Pane) (provider.Reading, error) {
	id, ok := pane.AgentSession.ID()
	if !ok {
		return provider.Reading{}, ErrNoSession
	}
	home, err := provider.Home(p.home, ".hermes")
	if err != nil {
		return provider.Reading{}, err
	}
	s, err := open(ctx, filepath.Join(home, "state.db"))
	if err != nil {
		return provider.Reading{Mode: provider.ResolutionAgentSession}, err
	}
	defer s.Close()
	reading, err := s.readOccupancy(ctx, id)
	reading.Mode = provider.ResolutionAgentSession
	return reading, err
}
