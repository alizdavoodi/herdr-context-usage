package opencode

import (
	"fmt"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

var (
	ErrNoOccupancy   = fmt.Errorf("opencode %w", provider.ErrNoOccupancy)
	ErrJustCompacted = fmt.Errorf("opencode %w", provider.ErrJustCompacted)
)

// Superseded lists this provider's answers, beyond the shared lifecycle
// outcomes, that prove the displayed occupancy wrong and clear it; anything
// left out preserves the value.
//
// fs.ErrNotExist is deliberately absent. Unlike the file-backed providers, this
// one opens its store through sqlitestore, which turns a missing or
// uninitialised store into ErrMissing or ErrSchema when it opens the database,
// so a raw missing-file error reaching here would be a bug rather than a
// lifecycle state.
func (opencodeProvider) Superseded() []error {
	return []error{
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
