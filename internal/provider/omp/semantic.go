package omp

import (
	"io/fs"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// Superseded lists this provider's answers that prove the displayed occupancy
// wrong and clear it; anything left out preserves the value.
func (ompProvider) Superseded() []error {
	return []error{
		ErrNoSession,
		provider.ErrNoHome,
		ErrNoOccupancy,
		ErrJustCompacted,
		// resolve hands back a transcript path rather than a file, so a missing
		// file is this provider's evidence that the agent session is gone or has
		// not been written yet.
		fs.ErrNotExist,
	}
}
