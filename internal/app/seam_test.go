package app

import (
	"errors"
	"testing"
)

// Every provider shares one classification, so the one mistake it cannot catch
// on its own is a provider declaring the same answer as both wrong and merely
// behind. Classify resolves that towards clearing the pane, which would blank a
// live value silently. Iterating the registry rather than a hand-written list
// means a newly registered provider is covered here without anyone remembering
// to add it.
func TestProviderStatesAreDisjoint(t *testing.T) {
	t.Parallel()

	for _, registered := range allProviders {
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

	seen := make(map[string]struct{}, len(allProviders))
	for _, registered := range allProviders {
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

// contextState compares a read failure against this sentinel to tell a
// compaction apart from an unreadable pane. A nil sentinel silently makes every
// compaction look like an ordinary failure, which preserves a pre-compaction
// value instead of clearing it.
func TestProvidersDeclareAJustCompactedSentinel(t *testing.T) {
	t.Parallel()

	for _, registered := range allProviders {
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

	for _, registered := range allProviders {
		if !registered.Kind().Implemented() {
			t.Errorf("provider %s is registered but Implemented() denies it", registered.Kind())
		}
	}
}
