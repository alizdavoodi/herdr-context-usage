package opencode2

import (
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

// Superseded lists this provider's answers that prove the displayed occupancy
// wrong and clear it; anything left out preserves the value.
//
// fs.ErrNotExist is deliberately absent, for the same reason as in the v1
// provider: sqlitestore turns a missing or uninitialised store into ErrMissing
// or ErrSchema when it opens the database, so a raw missing-file error reaching
// here would be a bug.
func (opencode2Provider) Superseded() []error {
	return []error{
		ErrNoSession, provider.ErrNoHome, ErrNoOccupancy, ErrJustCompacted,
		// A database without session_v2 and session_message is one opencode2 has
		// not initialised yet, the same lifecycle state as no file at all.
		sqlitestore.ErrMissing, sqlitestore.ErrSchema,
		// A pane whose title cannot name a session is a pane we must blank,
		// whichever of the four reasons applies. The reasons are separate so the
		// blank is diagnosable; the classification is the same for all of them.
		opencodeshared.ErrUntitled,
		opencodeshared.ErrTitleUnavailable,
		opencodeshared.ErrPluginTitle,
		opencodeshared.ErrNoTitleMatch,
	}
}
