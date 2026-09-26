package omp

import (
	"io/fs"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

// SemanticStates declares what each of this provider's answers proves about the
// occupancy already displayed: superseded answers clear it, stale-low answers
// leave it standing as a lower bound, and anything undeclared is transient.
func SemanticStates() report.States {
	return report.States{
		Superseded: []error{
			ErrNoSession,
			provider.ErrNoHome,
			ErrNoOccupancy,
			ErrJustCompacted,
			// Resolve hands back a transcript path rather than a file, so a missing
			// file is this provider's evidence that the agent session is gone or has
			// not been written yet.
			fs.ErrNotExist,
		},
		// omp never surfaces a turn in flight as an error: the walk skips every
		// record that carries no usage and reports the previous reading itself, so
		// an assistant record still being written leaves the last completed turn
		// standing. A transient permission or I/O failure is undeclared and
		// therefore preserves the displayed value.
		StaleLow: nil,
	}
}
