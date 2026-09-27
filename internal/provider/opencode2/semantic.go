package opencode2

import (
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
)

// Superseded lists this provider's answers that prove the displayed occupancy
// wrong and clear it; anything left out preserves the value.
//
// fs.ErrNotExist is deliberately absent, for the same reason as in the v1
// provider: a missing or uninitialised store becomes ErrStoreNotReady when the
// database is opened, so nothing downstream should ever see the raw error.
func (opencode2Provider) Superseded() []error {
	return []error{
		provider.ErrNoHome, ErrNoOccupancy, ErrStoreNotReady, ErrJustCompacted,
		// A pane whose title cannot name a session is a pane we must blank,
		// whichever of the four reasons applies. The reasons are separate so the
		// blank is diagnosable; the classification is the same for all of them.
		opencodeshared.ErrUntitled,
		opencodeshared.ErrTitleUnavailable,
		opencodeshared.ErrPluginTitle,
		opencodeshared.ErrNoTitleMatch,
	}
}
