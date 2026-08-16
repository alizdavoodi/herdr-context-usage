package report

import (
	"errors"
	"io/fs"
)

// SupersedesCurrentValue identifies states that prove the displayed value no
// longer belongs to the current session. Providers express unknown readings as
// errors so every successful numeric reading, including zero, remains valid.
func SupersedesCurrentValue(err error, semanticStates []error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	for _, state := range semanticStates {
		if errors.Is(err, state) {
			return true
		}
	}
	return false
}
