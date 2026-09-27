package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoHome means the host has no home directory for an agent's store to sit
// under. It is a fact about the host, not about any agent's store, and no agent
// release can change it, which is why it is declared once here rather than in
// each provider or in piompshared. That package's cap is written in terms of
// measurement — a name there has to be measured identical on both agents — and
// an unset $HOME produces nothing to measure on either, so it could only have
// been admitted there by exception, which is what erodes a cap whose only
// enforcement is that it has never been crossed.
//
// It is permanent for the life of the process, not momentary: there is nowhere
// to look and every later refresh reads the same way. Every provider therefore
// declares it superseded, so the displayed occupancy clears; an undeclared
// answer would preserve it, standing as if it were current.
var ErrNoHome = errors.New("agent home cannot be resolved")

// Home returns explicit when it is set, and otherwise defaultRel joined under
// the user's home directory.
//
// It reads no override variable of its own. main is the one reader of each
// agent's override and passes what it read as explicit, so a second read here
// would be a precedence tier nothing could reach.
func Home(explicit, defaultRel string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Both are wrapped: ErrNoHome is what every provider classifies on, and the
		// operating system's own text is the maintenance diagnostic. Classifying on
		// that text instead would bind every provider to a message that reads
		// "$HOME is not defined" on unix and something else elsewhere.
		return "", fmt.Errorf("%w: %w", ErrNoHome, err)
	}
	return filepath.Join(home, defaultRel), nil
}
