package app

import (
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// A provider that declares no superseded answer can never clear its pane, so a
// compaction or a session the pane has left would keep a number presented as
// current. Iterating the registry rather than a hand-written list means a newly
// registered provider is covered here without anyone remembering to add it.
func TestEveryRegisteredProviderDeclaresASupersededAnswer(t *testing.T) {
	t.Parallel()

	for kind, registered := range registry(Config{}) {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			declared := registered.Superseded()
			if len(declared) == 0 {
				t.Fatal("Superseded() declares no answer; every provider has at least one")
			}
		})
	}
}

// Registering a provider is necessary to make an agent kind live but not
// sufficient: dispatch reaches the registry only through FromAgent, which parses
// herdr's agent label into a Kind. A provider registered under a kind FromAgent
// never returns is unreachable, and the other property test here still passes
// — the registry is internally consistent, it is just never consulted. The row
// then blanks permanently with no error anywhere to explain it.
//
// Deriving the label from the kind is what makes this hold for a provider nobody
// has written yet. The opencode arm reads the process list to choose between its
// two versions, and returns the v1 kind when that list is empty, which is what
// the nil list below asks for.
func TestEveryRegisteredKindIsReachableFromItsAgentLabel(t *testing.T) {
	t.Parallel()

	for kind := range registry(Config{}) {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			got := provider.FromAgent(string(kind), nil)
			if got != kind {
				t.Fatalf("FromAgent(%q) = %q; the provider registered for %q is unreachable", string(kind), got, kind)
			}
		})
	}
}
