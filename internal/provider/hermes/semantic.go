package hermes

import (
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

// Superseded lists this provider's answers that prove the displayed occupancy
// wrong and clear it; anything left out preserves the value.
func (hermesProvider) Superseded() []error {
	return []error{
		provider.ErrNoHome, ErrNoSession, ErrSessionEnded, ErrNoOccupancy, ErrJustCompacted,
		// A database without sessions and messages is one Hermes has not
		// initialised yet, the same lifecycle state as no file at all.
		sqlitestore.ErrMissing, sqlitestore.ErrSchema,
	}
}
