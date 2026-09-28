package opencode2

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
)

// New returns the opencode2 provider. explicitDB and dataHome are the values
// main read from OPENCODE2_DB and XDG_DATA_HOME, empty for opencode's own
// default under the user's home. Both are resolved per refresh rather than
// here, so a host with no home blanks the pane instead of failing construction.
func New(explicitDB, dataHome string) provider.Provider {
	return opencode2Provider{db: explicitDB, dataHome: dataHome}
}

type opencode2Provider struct{ db, dataHome string }

// Measure tries the session id herdr reported, then the pane's directory and the
// TUI's terminal title, the same ladder as the v1 provider.
//
// herdr 0.9.1's opencode integration (v12) reports, for opencode2, the root of
// the session the pane's TUI has selected. It reports nothing on the TUI's home
// screen, so herdr keeps the last session there, which the README records
// under "When an opencode row never fills in" beside why the title remains for
// panes without that integration.
//
// The mode is set before the store opens, so a store that fails to open after
// a reported id still records agent_session.
func (p opencode2Provider) Measure(ctx context.Context, pane provider.Pane) (provider.Reading, error) {
	id, reported := pane.AgentSession.ID()
	var mode provider.ResolutionMode
	if reported {
		mode = provider.ResolutionAgentSession
	}
	s, err := open(ctx, p.db, p.dataHome)
	if err != nil {
		return provider.Reading{Mode: mode}, err
	}
	defer s.Close()
	if reported {
		if err := s.requireRootSession(ctx, id); err != nil {
			return provider.Reading{Mode: mode}, err
		}
	} else {
		// Root sessions live in session_v2, and a v2 plugin route interpolates its
		// plugin name into the same title shape v1 fills with a plugin id, so only a
		// positive root-session match can produce a reading.
		id, err = opencodeshared.ResolveTitle(ctx, s.DB, "session_v2", pane.ForegroundCWD, pane.TerminalTitle)
		if err != nil {
			return provider.Reading{}, attributeMiniInterface(err, pane.Processes)
		}
		mode = provider.ResolutionTerminalTitle
	}
	reading, err := s.readOccupancy(ctx, id)
	reading.Mode = mode
	return reading, err
}
