package opencode

import (
	"errors"
	"testing"
)

func TestResolveAcceptsOnlyNativeSessionID(t *testing.T) {
	t.Parallel()

	resolved, err := Resolve(SessionInput{AgentSession: &AgentSession{Kind: "id", Value: "ses_current"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.ID != "ses_current" || resolved.Mode != ResolutionAgentSession {
		t.Fatalf("Resolve() = %#v", resolved)
	}
}

func TestResolveDoesNotGuessFromDirectoryOrPath(t *testing.T) {
	t.Parallel()

	for _, input := range []SessionInput{
		{},
		{AgentSession: &AgentSession{Kind: "path", Value: "/work/project"}},
		{AgentSession: &AgentSession{Kind: "id"}},
	} {
		if _, err := Resolve(input); !errors.Is(err, ErrNoSession) {
			t.Fatalf("Resolve(%#v) error = %v, want ErrNoSession", input, err)
		}
	}
}
