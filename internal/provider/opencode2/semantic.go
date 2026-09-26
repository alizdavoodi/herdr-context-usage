package opencode2

import (
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

// States declares what each of this provider's answers proves about the
// occupancy already displayed: superseded answers clear it, stale-low answers
// leave it standing as a lower bound, and anything undeclared is transient.
//
// fs.ErrNotExist is deliberately absent, for the same reason as in the v1
// provider: a missing or uninitialised store becomes ErrStoreNotReady when the
// database is opened, so nothing downstream should ever see the raw error.
func (opencode2Provider) States() report.States {
	return report.States{
		Superseded: []error{
			provider.ErrNoHome, ErrNoOccupancy, ErrStoreNotReady, ErrJustCompacted,
			// A pane whose title cannot name a session is a pane we must blank,
			// whichever of the four reasons applies. The reasons are separate so the
			// blank is diagnosable; the classification is the same for all of them.
			opencodeshared.ErrUntitled,
			opencodeshared.ErrTitleUnavailable,
			opencodeshared.ErrPluginTitle,
			opencodeshared.ErrNoTitleMatch,
		},
		// A turn in flight never surfaces as an error here: readOccupancy crosses
		// in-flight assistant rows and reports the previous completed reading.
		StaleLow: nil,
	}
}
