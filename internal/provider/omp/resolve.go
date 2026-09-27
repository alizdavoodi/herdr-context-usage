package omp

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

var ErrNoSession = errors.New("omp agent session cannot be resolved")

// resolve turns herdr's reported agent session into a transcript path.
//
// It takes no working directory and no process list, and must not grow either,
// though the pane offers both and claude reads them. There is no
// working-directory fallback for three reasons, any one of which is sufficient:
// omp files an agent session under a directory derived from the one it started
// in, so a pane that has since moved resolves to nothing or to an unrelated
// agent session; the transcript does not mark a sub-agent session, so a
// directory-scoped match has nothing to exclude one with; and recency is banned
// outright by ADR-0004, which measured it picking the wrong session on the only
// live sample available. Wiring either in by analogy with claude would trade a
// blank row for a wrong number.
//
// There is deliberately no sub-agent filter and no parent-session field is read:
// omp's integration gates on the agent's root session and on interactive mode,
// so a sub-agent session never reports an agent session and a headless run never
// reports at all. The main-thread invariant is enforced upstream of this plugin,
// which is why adding a transcript-level filter here would only add a way to be
// wrong.
func resolve(home string, session *provider.AgentSession) (string, error) {
	if path, ok := session.Path(); ok {
		return path, nil
	}
	id, ok := session.ID()
	if !ok {
		return "", ErrNoSession
	}

	// Only the id match needs the store, so the home is resolved here rather than
	// up front: a pane reporting a path is fully resolvable without one, and
	// looking it up early would let a failing home lookup leave that pane
	// unmeasured for a directory it never needed.
	//
	// The default is omp's own home, not pi's. A shared default would point an
	// omp pane at pi's store on any machine running both, and silently, because
	// the two stores have the same layout below the home.
	home, err := provider.Home(home, filepath.Join(".omp", "agent"))
	if err != nil {
		return "", err
	}
	path, found, err := provider.SessionTranscript(home, id)
	if err != nil {
		return "", fmt.Errorf("find omp transcripts: %w", err)
	}
	if !found {
		return "", ErrNoSession
	}
	return path, nil
}
