package pi

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

var ErrNoSession = errors.New("pi agent session cannot be resolved")

// sessionInput carries no working directory and no process list, and must not
// grow either, though the pane offers both and claude reads them. There is no
// working-directory fallback for three reasons, any one of which is sufficient:
// pi files an agent session under the directory it started in, so a pane that
// has since moved resolves to nothing or to an unrelated agent session —
// measured on this machine, where the derived directory for this repository
// exists and is empty while the real agent session sits elsewhere; the
// transcript does not mark a sub-agent session, so a directory-scoped match has
// nothing to exclude one with; and recency is banned outright by ADR-0004, which
// measured it picking the wrong session on the only live sample available.
// Wiring either in by analogy with claude would trade a blank row for a wrong
// number.
type sessionInput struct {
	PiHome       string
	AgentSession *provider.AgentSession
}

// resolvedSession always carries provider.ResolutionAgentSession, because
// resolution has one source: herdr's reported agent session is the whole of the
// input, so the branches of resolve differ only in how they read one value, and
// a second mode would name a stage that does not exist.
type resolvedSession struct {
	Path string
	Mode provider.ResolutionMode
}

// resolve turns herdr's reported agent session into a transcript path.
//
// There is deliberately no sub-agent filter and no parent-session field is read:
// pi's integration gates on the agent's root session and on interactive mode, so
// a sub-agent session never reports an agent session and a headless run never
// reports at all. The main-thread invariant is enforced upstream of this plugin,
// which is why adding a transcript-level filter here would only add a way to be
// wrong.
func resolve(input sessionInput) (resolvedSession, error) {
	if path, ok := input.AgentSession.Path(); ok {
		return resolvedSession{Path: path, Mode: provider.ResolutionAgentSession}, nil
	}
	id, ok := input.AgentSession.ID()
	if !ok {
		return resolvedSession{}, ErrNoSession
	}

	// Only the id match needs the store, so the home is resolved here rather than
	// up front: a pane reporting a path is fully resolvable without one, and
	// looking it up early would let a failing home lookup leave that pane
	// unmeasured for a directory it never needed.
	home, err := provider.Home(input.PiHome, filepath.Join(".pi", "agent"))
	if err != nil {
		return resolvedSession{}, err
	}
	path, err := uniqueTranscript(home, id)
	if err != nil {
		return resolvedSession{}, err
	}
	return resolvedSession{Path: path, Mode: provider.ResolutionAgentSession}, nil
}

// uniqueTranscript finds the transcript whose file name ends with id.
//
// The match is by name only. No ordering may decide between candidates — not
// mtime, not name, not the start timestamp the file name carries — so two
// matches are treated as unresolved rather than picked between, and a blank row
// is the answer. Recency in particular is banned outright by ADR-0004, which
// measured it picking the wrong session.
func uniqueTranscript(home, id string) (string, error) {
	if !isSessionID(id) {
		return "", ErrNoSession
	}
	// The two wildcards keep the pattern anchored to one directory below
	// sessions/ and to the end of the file name, so a partial segment cannot
	// match.
	matches, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*"+id+".jsonl"))
	if err != nil {
		return "", fmt.Errorf("find pi transcripts: %w", err)
	}
	if len(matches) != 1 {
		return "", ErrNoSession
	}
	return matches[0], nil
}

// isSessionID rejects anything that cannot be a pi session id. A real one is a
// UUID; a value carrying a glob metacharacter or a path separator would instead
// widen or escape the pattern above, so it is refused before it is interpolated.
func isSessionID(id string) bool {
	return id != "" && !strings.ContainsAny(id, `*?[]\/`)
}
