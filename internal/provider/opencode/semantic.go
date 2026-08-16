package opencode

import (
	"errors"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

var (
	ErrNoOccupancy   = errors.New("opencode agent session has no current context occupancy")
	ErrNoSchema      = errors.New("opencode v1 database schema is unavailable")
	ErrStoreNotReady = errors.New("opencode v1 store has not been written yet")
	ErrJustCompacted = errors.New("opencode agent session was just compacted")
)

// SemanticStates declares what each of this provider's answers proves about the
// occupancy already displayed: superseded answers clear it, stale-low answers
// leave it standing as a lower bound, and anything undeclared is transient.
//
// fs.ErrNotExist is deliberately absent. Unlike the two file-backed providers,
// this one turns a missing or uninitialised store into ErrStoreNotReady or
// ErrNoSchema at the point it opens the database, so a raw missing-file error
// reaching here would be a bug rather than a lifecycle state.
func SemanticStates() report.States {
	return report.States{
		Superseded: []error{
			ErrNoSession, ErrNoOccupancy, ErrNoSchema, ErrStoreNotReady, ErrJustCompacted,
			// A pane whose title cannot name a session is a pane we must blank,
			// whichever of the four reasons applies. The reasons are separate so the
			// blank is diagnosable; the classification is the same for all of them.
			opencodeshared.ErrUntitled,
			opencodeshared.ErrTitleUnavailable,
			opencodeshared.ErrPluginTitle,
			opencodeshared.ErrNoTitleMatch,
		},
		// A turn in flight never surfaces as an error here: ReadOccupancy walks
		// back to the previous completed assistant message and reports it.
		StaleLow: nil,
	}
}
