package hermes

import (
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// Superseded lists this provider's answers that prove the displayed occupancy
// wrong and clear it; anything left out preserves the value.
func (hermesProvider) Superseded() []error {
	return []error{
		provider.ErrNoHome, ErrNoSession, ErrSessionEnded,
		ErrStoreNotReady, ErrNoOccupancy, ErrJustCompacted,
	}
}
