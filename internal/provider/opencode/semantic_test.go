package opencode

import (
	"errors"
	"testing"
)

func TestSemanticStatesDeclaresEverySupersedingState(t *testing.T) {
	t.Parallel()

	states := SemanticStates()
	for _, want := range []error{ErrNoSession, ErrUntitled, ErrNoOccupancy, ErrNoSchema, ErrStoreNotReady} {
		found := false
		for _, state := range states {
			if errors.Is(state, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("SemanticStates() = %v, missing %v", states, want)
		}
	}
}
