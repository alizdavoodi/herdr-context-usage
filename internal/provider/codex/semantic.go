package codex

// SemanticStates declares every answer that proves a previous measurement no
// longer describes the current agent session.
func SemanticStates() []error {
	return []error{ErrNoSession, ErrNoOccupancy, ErrJustCompacted}
}
