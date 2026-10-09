package codex

import (
	"io/fs"
)

// Superseded lists this provider's answers, beyond the shared lifecycle
// outcomes, that prove the displayed occupancy wrong and clear it; anything
// left out, ErrFailedTurn included, preserves it.
func (codexProvider) Superseded() []error {
	return []error{
		// Codex names a rollout before it writes it, so the window between
		// resolving a path and the file existing is a real lifecycle state and
		// not an I/O failure.
		fs.ErrNotExist,
	}
}
