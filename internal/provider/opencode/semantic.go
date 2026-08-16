package opencode

import "errors"

var (
	ErrNoOccupancy = errors.New("opencode agent session has no current context occupancy")
	ErrNoSchema    = errors.New("opencode v1 database schema is unavailable")
)

// SemanticStates declares every answer that proves a previous measurement no
// longer describes the current agent session.
func SemanticStates() []error {
	return []error{ErrNoSession, ErrNoOccupancy, ErrNoSchema}
}
