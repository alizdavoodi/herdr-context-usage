package hermes

import (
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

// States declares what each of this provider's answers proves about the
// occupancy already displayed: superseded answers clear it, stale-low answers
// leave it standing as a lower bound, and anything undeclared is transient.
func (hermesProvider) States() report.States {
	return report.States{
		Superseded: []error{
			provider.ErrNoHome, ErrNoSession, ErrSessionEnded,
			ErrStoreNotReady, ErrNoOccupancy, ErrJustCompacted,
		},
		// A turn in flight never surfaces as an error: until the call completes
		// the anchor still holds the previous call's prompt, a valid lower bound
		// that is reported as the reading itself.
		StaleLow: nil,
	}
}
