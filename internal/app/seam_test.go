package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/claude"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/codex"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/hermes"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/omp"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/pi"
)

// A provider that declares no superseded answer can never clear its pane, so a
// compaction or a session the pane has left would keep a number presented as
// current. The just-compacted answer is required by name: refreshOne recognises
// that state only through provider.ErrJustCompacted, so a provider whose own
// sentinel does not wrap it records unknown where it means just_compacted, and
// one that does not declare it superseded leaves the pre-compaction number up.
// Iterating the registry rather than a hand-written list means a newly
// registered provider is covered here without anyone remembering to add it.
func TestEveryRegisteredProviderDeclaresASupersededAnswer(t *testing.T) {
	t.Parallel()

	for kind, registered := range Registry(Config{}) {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			declared := registered.Superseded()
			if len(declared) == 0 {
				t.Fatal("Superseded() declares no answer; every provider has at least one")
			}
			if !slices.ContainsFunc(declared, func(e error) bool { return errors.Is(e, provider.ErrJustCompacted) }) {
				t.Fatal("Superseded() declares no answer wrapping provider.ErrJustCompacted; a provider whose own sentinel does not wrap the root records unknown instead of just_compacted")
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
//
// provider.None is refused outright. FromAgent returns it for every label it
// does not recognise, so a provider registered there would measure every
// unrecognised agent, and the reachability check is vacuous for it:
// FromAgent("", nil) is None as well.
func TestEveryRegisteredKindIsReachableFromItsAgentLabel(t *testing.T) {
	t.Parallel()

	for kind := range Registry(Config{}) {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			if kind == provider.None {
				t.Fatal("a provider is registered under provider.None, the kind every unrecognised agent label dispatches to")
			}
			got := provider.FromAgent(string(kind), nil)
			if got != kind {
				t.Fatalf("FromAgent(%q) = %q; the provider registered for %q is unreachable", string(kind), got, kind)
			}
		})
	}
}

// The other tests here pin that every registered kind is reachable and declares
// a superseded answer, and both still pass when two entries are swapped: a
// provider.Omp entry built by pi.New is reachable and well-formed, and it reads
// an omp pane against pi's store. A swap fails only a test that asks which
// provider answered. An empty pane makes each of these providers refuse before
// it opens any store, and each refuses with its own package's sentinel, so the
// sentinel's identity is the only thing that ties an entry to its constructor.
//
// opencode and opencode2 are absent because they cannot be told apart this way.
// Both open the store before resolving anything, and every refusal either
// returns is shared from opencodeshared or sqlitestore.
func TestEachRegistryEntryIsItsOwnKindsProvider(t *testing.T) {
	t.Parallel()

	own := map[provider.Kind]error{
		provider.Claude: claude.ErrNoSession,
		provider.Codex:  codex.ErrNoSession,
		provider.Pi:     pi.ErrNoSession,
		provider.Omp:    omp.ErrNoSession,
		provider.Hermes: hermes.ErrNoSession,
	}
	registry := Registry(Config{})
	for kind, want := range own {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			registered, ok := registry[kind]
			if !ok {
				t.Fatalf("no provider registered for %q", kind)
			}
			if _, err := registered.Measure(context.Background(), provider.Pane{}); !errors.Is(err, want) {
				t.Fatalf("Measure(empty pane) error = %v, want %v; the entry for %q is another kind's provider", err, want, kind)
			}
		})
	}
}
