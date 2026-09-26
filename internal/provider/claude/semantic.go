package claude

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
			// file is this provider's evidence that the session is gone or has not
			// been written yet. report used to special-case it for everybody, which
			// hid the fact that it is provider semantics.
			fs.ErrNotExist,
		},
		// Claude never surfaces a turn in flight as an error: ReadOccupancy walks
		// back across the zero-usage rows a failed turn leaves behind and reports
		// the previous reading itself.
		StaleLow: nil,
	}
}
