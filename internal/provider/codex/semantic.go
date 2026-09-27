package codex

import (
	"io/fs"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// Superseded lists this provider's answers that prove the displayed occupancy
// wrong and clear it; anything left out, ErrFailedTurn included, preserves it.
func (codexProvider) Superseded() []error {
	return []error{
		ErrNoSession,
		provider.ErrNoHome,
		ErrNoOccupancy,
		ErrJustCompacted,
		// Codex names a rollout before it writes it, so the window between
		// resolving a path and the file existing is a real lifecycle state and
		// not an I/O failure.
		fs.ErrNotExist,
	}
}
