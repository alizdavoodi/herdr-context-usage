package claude

import (
	"io/fs"
)

// Superseded lists this provider's answers, beyond the shared lifecycle
// outcomes, that prove the displayed occupancy wrong and clear it; anything
// left out preserves the value.
func (claudeProvider) Superseded() []error {
	return []error{
		// resolve hands back a transcript path rather than a file, so a missing
		// file is this provider's evidence that the session is gone or has not
		// been written yet.
		fs.ErrNotExist,
	}
}
