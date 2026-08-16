package opencode2

import (
	"errors"
	"testing"
)

func TestSemanticStatesDeclaresEverySupersedingState(t *testing.T) {
	t.Parallel()

	states := SemanticStates()
	for _, want := range []error{ErrNoSession, ErrUntitled, ErrNoOccupancy, ErrStoreNotReady, ErrJustCompacted} {
		if !containsError(states, want) {
			t.Fatalf("SemanticStates() = %v, missing %v", states, want)
		}
	}
}

func containsError(states []error, target error) bool {
	for _, state := range states {
		if errors.Is(state, target) {
			return true
		}
	}
	return false
}
