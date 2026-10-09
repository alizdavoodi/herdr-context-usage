package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoHome means the host has no home directory for an agent's store to sit
// under. It is a fact about the host, not about any agent's store, and no agent
// release can change it, which is why it is declared once here rather than in
// each provider.
//
// It is permanent for the life of the process, not momentary: there is nowhere
// to look and every later refresh reads the same way. IsSuperseded recognizes it
// as one of the shared lifecycle outcomes, so the displayed occupancy clears for
// every provider without each one declaring it; an undeclared answer would
// preserve it, standing as if it were current.
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

// SessionTranscript finds the transcript under home whose file name ends with
// id, the layout pi and omp share: sessions/<directory>/<start>_<id>.jsonl. It
// reports false, with no error, when no single transcript answers to the id.
//
// The match is by name only. No ordering may decide between candidates — not
// mtime, not name, not the start timestamp the file name carries — so two
// matches are unresolved rather than picked between, and a blank row is the
// answer. Recency in particular is banned outright by ADR-0004, which measured
// it picking the wrong session.
//
// The two wildcards anchor the pattern to one directory below sessions/ and to
// the end of the file name, so a partial segment cannot match. The same
// anchoring keeps out what omp writes beside each transcript: a directory of
// per-tool log files named for the same start timestamp and id, which three of
// the six transcripts in the real directory have — the agent sessions that ran
// a tool. Depth excludes its files, which sit two levels below sessions/, and
// the .jsonl suffix excludes the directory itself.
//
// A real id is a UUID. One carrying a glob metacharacter or a path separator
// would widen or escape the pattern, so it is refused before it is interpolated.
func SessionTranscript(home, id string) (string, bool, error) {
	if id == "" || strings.ContainsAny(id, `*?[]\/`) {
		return "", false, nil
	}
	matches, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*"+id+".jsonl"))
	if err != nil {
		return "", false, err
	}
	if len(matches) != 1 {
		return "", false, nil
	}
	return matches[0], true, nil
}
