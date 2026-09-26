package opencode

import (
	"errors"
	"strings"

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

func Resolve(input SessionInput) (ResolvedSession, error) {
	if input.AgentSession == nil || input.AgentSession.Value == "" || strings.ToLower(input.AgentSession.Kind) != "id" {
		return ResolvedSession{}, ErrNoSession
	}
	return ResolvedSession{ID: input.AgentSession.Value, Mode: ResolutionAgentSession}, nil
}
