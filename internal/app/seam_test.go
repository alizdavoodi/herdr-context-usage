package app

import (
	"errors"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider/claude"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/codex"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencode"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencode2"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

// The four providers share one classification, so the one mistake it cannot
// catch on its own is a provider declaring the same answer as both wrong and
// merely behind. Classify resolves that towards clearing the pane, which would
// blank a live value silently; this is the only place all four declarations are
// visible at once, so the contradiction is caught here.
func TestProviderStatesAreDisjoint(t *testing.T) {
	t.Parallel()

	states := map[string]report.States{
		"claude":    claude.SemanticStates(),
		"codex":     codex.SemanticStates(),
		"opencode":  opencode.SemanticStates(),
		"opencode2": opencode2.SemanticStates(),
	}

	for name, declared := range states {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
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
