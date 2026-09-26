package codex

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
			// Codex names a rollout before it writes it, so the window between
			// resolving a path and the file existing is a real lifecycle state and
			// not an I/O failure. report used to special-case this for every
			// provider, which is why codex classified correctly without ever
			// declaring it; the declaration has to live here now.
			fs.ErrNotExist,
		},
		// A turn that recorded no usage leaves the previous reading valid: this
		// provider reads only the newest usage row rather than walking back, so
		// the reading already on the pane is the lower bound.
		StaleLow: []error{ErrFailedTurn},
	}
}
