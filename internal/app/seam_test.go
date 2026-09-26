package app

import (
	"errors"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// Every provider shares one classification, so the one mistake it cannot catch
// on its own is a provider declaring the same answer as both wrong and merely
// behind. Classify resolves that towards clearing the pane, which would blank a
// live value silently. Iterating the registry rather than a hand-written list
// means a newly registered provider is covered here without anyone remembering
// to add it.
func TestProviderStatesAreDisjoint(t *testing.T) {
	t.Parallel()

	for _, registered := range registry(Config{}) {
		t.Run(string(registered.Kind()), func(t *testing.T) {
			t.Parallel()
			declared := registered.States()
			if len(declared.Superseded) == 0 {
				t.Fatal("SemanticStates() declares no superseded answer; every provider has at least one")
			}
			for _, superseded := range declared.Superseded {
				for _, staleLow := range declared.StaleLow {
					if errors.Is(superseded, staleLow) || errors.Is(staleLow, superseded) {
						t.Fatalf("%v is declared both superseded and stale-low", superseded)
					}
				}
			}
		})
	}
}

// providerFor returns the first match, so two providers claiming one kind would
// leave the second permanently unreachable with nothing to indicate it.
func TestRegistryHasNoDuplicateKinds(t *testing.T) {
	t.Parallel()

	providers := registry(Config{})
	seen := make(map[string]struct{}, len(providers))
	for _, registered := range providers {
		kind := string(registered.Kind())
		if kind == "" {
			t.Error("a provider reports an empty kind, which no agent label can match")
			continue
		}
		if _, duplicate := seen[kind]; duplicate {
			t.Errorf("two providers claim kind %q; only the first is reachable", kind)
		}
		seen[kind] = struct{}{}
	}
}

// Registering a provider is necessary to make an agent kind live but not
// sufficient: dispatch reaches the registry only through FromAgent, which parses
// herdr's agent label into a Kind. A provider registered under a kind FromAgent
// never returns is unreachable, and every other property test here still passes
// — the registry is internally consistent, it is just never consulted. The row
// then blanks permanently with no error anywhere to explain it.
//
// Deriving the label from the kind is what makes this hold for a provider nobody
// has written yet. The opencode arm needs an available process list to choose
// between its two versions, and returns the v1 kind when that list is empty,
// which is what the empty ProcessList below asks for.
func TestEveryRegisteredKindIsReachableFromItsAgentLabel(t *testing.T) {
	t.Parallel()

	for _, registered := range registry(Config{}) {
		kind := registered.Kind()
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			got := provider.FromAgent(string(kind), provider.ProcessList{Available: true})
			if got != kind {
				t.Fatalf("FromAgent(%q) = %q; the provider registered for %q is unreachable", string(kind), got, kind)
			}
		})
	}
}

// contextState compares a read failure against this sentinel to tell a
// compaction apart from an unreadable pane. A nil sentinel silently makes every
// compaction look like an ordinary failure, which preserves a pre-compaction
// value instead of clearing it.
func TestProvidersDeclareAJustCompactedSentinel(t *testing.T) {
	t.Parallel()

	for _, registered := range registry(Config{}) {
		if registered.JustCompacted() == nil {
			t.Errorf("provider %s declares no just-compacted sentinel", registered.Kind())
		}
	}
}

// Implemented no longer gates dispatch — the registry does — but it stays
// public and still reads as the answer to whether this plugin can measure a
// kind. Pinning it to the registry keeps it from drifting into a claim the code
// cannot honour. The opposite direction needs no test: a kind Implemented but
// unregistered now clears its row rather than keeping a stale value.
func TestImplementedAgreesWithRegistry(t *testing.T) {
	t.Parallel()

	for _, registered := range registry(Config{}) {
		if !registered.Kind().Implemented() {
			t.Errorf("provider %s is registered but Implemented() denies it", registered.Kind())
		}
	}
}
