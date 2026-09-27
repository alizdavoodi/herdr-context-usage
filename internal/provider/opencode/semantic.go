package opencode

import (
	"errors"
	"fmt"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

var (
	// ErrNoSession is a resolved session id that names no session in the store,
	// whether herdr reported it or the pane's title matched it: the session has
	// been deleted, or herdr reported one this store never held. The reasons a
	// terminal title cannot identify a session are separate, and live in
	// opencodeshared where both providers share them.
	ErrNoSession     = errors.New("opencode agent session cannot be resolved")
	ErrNoOccupancy   = errors.New("opencode agent session has no current context occupancy")
	ErrJustCompacted = fmt.Errorf("opencode %w", provider.ErrJustCompacted)
)

// Superseded lists this provider's answers that prove the displayed occupancy
// wrong and clear it; anything left out preserves the value.
//
// fs.ErrNotExist is deliberately absent. Unlike the file-backed providers, this
// one opens its store through sqlitestore, which turns a missing or
// uninitialised store into ErrMissing or ErrSchema when it opens the database,
// so a raw missing-file error reaching here would be a bug rather than a
// lifecycle state.
func (opencodeProvider) Superseded() []error {
	return []error{
		ErrNoSession, provider.ErrNoHome, ErrNoOccupancy, ErrJustCompacted,
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
