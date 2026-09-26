package opencode

import (
	"errors"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

func TestResolveAcceptsOnlyNativeSessionID(t *testing.T) {
	t.Parallel()

	// An unlabelled value that is not an absolute path is an id by the shared
	// rule, and a miss in the store blanks the row like any other unknown id.
	for _, session := range []provider.AgentSession{
		{Kind: "id", Value: "ses_current"},
		{Value: "ses_current"},
	} {
		resolved, err := Resolve(SessionInput{AgentSession: &session})
		if err != nil {
			t.Fatalf("Resolve(%#v) error = %v", session, err)
		}
		if resolved.ID != "ses_current" || resolved.Mode != ResolutionAgentSession {
			t.Fatalf("Resolve(%#v) = %#v", session, resolved)
		}
	}
}

func TestResolveDoesNotGuessFromDirectoryOrPath(t *testing.T) {
	t.Parallel()

	for _, input := range []SessionInput{
		{},
		{AgentSession: &provider.AgentSession{Kind: "path", Value: "/work/project"}},
		{AgentSession: &provider.AgentSession{Value: "/work/project"}},
		{AgentSession: &provider.AgentSession{Kind: "id"}},
	} {
		if _, err := Resolve(input); !errors.Is(err, ErrNoSession) {
			t.Fatalf("Resolve(%#v) error = %v, want ErrNoSession", input, err)
		}
	}
}
