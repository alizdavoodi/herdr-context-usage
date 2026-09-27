package opencode

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

// Both tests point the provider at a store that has never been written, so
// resolution stops at opening it and the mode it hands back was decided by the
// reported session alone. That is the case the ladder has to get right: the
// record's resolution_mode is the only thing that tells an accepted id whose
// store is missing apart from a pane that resolved nothing.

func TestResolveAcceptsOnlyNativeSessionID(t *testing.T) {
	t.Parallel()

	// An unlabelled value that is not an absolute path is an id by the shared
	// rule, and a miss in the store blanks the row like any other unknown id.
	unwritten := filepath.Join(t.TempDir(), "opencode.db")
	for _, session := range []provider.AgentSession{
		{Kind: "id", Value: "ses_current"},
		{Value: "ses_current"},
	} {
		reading, err := New(unwritten, "").Measure(context.Background(), provider.Pane{AgentSession: &session})
		if !errors.Is(err, sqlitestore.ErrMissing) {
			t.Fatalf("Measure(%#v) error = %v, want sqlitestore.ErrMissing", session, err)
		}
		if reading.Mode != provider.ResolutionAgentSession {
			t.Fatalf("Measure(%#v) mode = %q, want agent_session kept past the open failure", session, reading.Mode)
		}
	}
}

func TestResolveDoesNotGuessFromDirectoryOrPath(t *testing.T) {
	t.Parallel()

	unwritten := filepath.Join(t.TempDir(), "opencode.db")
	for _, session := range []*provider.AgentSession{
		nil,
		{Kind: "path", Value: "/work/project"},
		{Value: "/work/project"},
		{Kind: "id"},
	} {
		reading, err := New(unwritten, "").Measure(context.Background(), provider.Pane{AgentSession: session})
		if !errors.Is(err, sqlitestore.ErrMissing) {
			t.Fatalf("Measure(%#v) error = %v, want sqlitestore.ErrMissing", session, err)
		}
		if reading.Mode != "" {
			t.Fatalf("Measure(%#v) mode = %q, want none: no stage resolved a session", session, reading.Mode)
		}
	}
}
