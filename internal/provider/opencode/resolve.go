package opencode

import (
	"errors"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
)

type ResolutionMode string

const ResolutionAgentSession ResolutionMode = "agent_session"

// Both opencode providers record the same mode for a title-resolved session.
const ResolutionTerminalTitle ResolutionMode = opencodeshared.ResolutionTerminalTitle

// ErrNoSession is herdr reporting no usable session id for this pane. The
// reasons a terminal title cannot identify a session are separate, and live in
// opencodeshared where both providers share them.
var ErrNoSession = errors.New("opencode agent session cannot be resolved")

type SessionInput struct {
	AgentSession *provider.AgentSession
}

type ResolvedSession struct {
	ID   string
	Mode ResolutionMode
}

// Resolve consults only a reported id. opencode keeps its sessions in a
// database rather than a file per session, so a reported path names nothing
// this provider can read and is refused.
func Resolve(input SessionInput) (ResolvedSession, error) {
	id, ok := input.AgentSession.ID()
	if !ok {
		return ResolvedSession{}, ErrNoSession
	}
	return ResolvedSession{ID: id, Mode: ResolutionAgentSession}, nil
}
