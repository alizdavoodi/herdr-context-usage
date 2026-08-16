package opencode

import (
	"errors"
	"strings"
)

type ResolutionMode string

const ResolutionAgentSession ResolutionMode = "agent_session"

var ErrNoSession = errors.New("opencode agent session cannot be resolved")

type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type SessionInput struct {
	AgentSession *AgentSession
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
