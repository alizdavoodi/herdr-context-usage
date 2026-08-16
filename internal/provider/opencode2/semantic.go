package opencode2

import "github.com/alizdavoodi/herdr-context-usage/internal/report"

// SemanticStates declares what each of this provider's answers proves about the
// occupancy already displayed: superseded answers clear it, stale-low answers
// leave it standing as a lower bound, and anything undeclared is transient.
//
// fs.ErrNotExist is deliberately absent, for the same reason as in the v1
// provider: a missing or uninitialised store becomes ErrStoreNotReady when the
// database is opened, so nothing downstream should ever see the raw error.
func SemanticStates() report.States {
	return report.States{
		Superseded: []error{ErrNoSession, ErrUntitled, ErrNoOccupancy, ErrStoreNotReady, ErrJustCompacted},
		// A turn in flight never surfaces as an error here: ReadOccupancy crosses
		// in-flight assistant rows and reports the previous completed reading.
		StaleLow: nil,
	}
}
