package omp

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// One mode, because resolution has one source. herdr's reported agent session is
// the whole of the input, so the branches below differ only in how they read one
// value and a second constant would name a stage that does not exist.
type ResolutionMode string

const ResolutionAgentSession ResolutionMode = "agent_session"

var ErrNoSession = errors.New("omp agent session cannot be resolved")

// SessionInput carries no working directory and no process list, and must not
// grow either. There is no working-directory fallback for three reasons, any one
// of which is sufficient: omp files an agent session under a directory derived
// from the one it started in, so a pane that has since moved resolves to nothing
// or to an unrelated agent session; the transcript does not mark a sub-agent
// session, so a directory-scoped match has nothing to exclude one with; and
// recency is banned outright by ADR-0004, which measured it picking the wrong
// session on the only live sample available.
//
// omp adds a fourth that pi does not have. omp ships no screen-detection
// manifest at all, so a pane running it is not labelled an agent by herdr until
// its integration is installed, and until then the pane never reaches this
// plugin to be resolved by any means. Installing the integration is a
// precondition rather than an improvement on a fallback that works without it.
type SessionInput struct {
	OmpHome      string
	AgentSession *provider.AgentSession
}

type ResolvedSession struct {
	Path string
	Mode ResolutionMode
}

// Resolve turns herdr's reported agent session into a transcript path.
//
// There is deliberately no sub-agent filter and no parent-session field is read:
// omp's integration gates on the agent's root session and on interactive mode,
// so a sub-agent session never reports an agent session and a headless run never
// reports at all. The main-thread invariant is enforced upstream of this plugin,
// which is why adding a transcript-level filter here would only add a way to be
// wrong.
func Resolve(input SessionInput) (ResolvedSession, error) {
	if path, ok := input.AgentSession.Path(); ok {
		return ResolvedSession{Path: path, Mode: ResolutionAgentSession}, nil
	}
	id, ok := input.AgentSession.ID()
	if !ok {
		return ResolvedSession{}, ErrNoSession
	}

	// Only the id match needs the store, so the home is resolved here rather than
	// up front: a pane reporting a path is fully resolvable without one, and
	// looking it up early would let a failing home lookup leave that pane
	// unmeasured for a directory it never needed.
	//
	// The default is omp's own home, not pi's. A shared default would point an
	// omp pane at pi's store on any machine running both, and silently, because
	// the two stores have the same layout below the home.
	home, err := provider.Home(input.OmpHome, filepath.Join(".omp", "agent"))
	if err != nil {
		return ResolvedSession{}, err
	}
	path, err := uniqueTranscript(home, id)
	if err != nil {
		return ResolvedSession{}, err
	}
	return ResolvedSession{Path: path, Mode: ResolutionAgentSession}, nil
}

// uniqueTranscript finds the transcript whose file name ends with id.
//
// The match is by name only. No ordering may decide between candidates — not
// mtime, not name, not the start timestamp the file name carries — so two
// matches are treated as unresolved rather than picked between, and a blank row
// is the answer. Recency in particular is banned outright by ADR-0004, which
// measured it picking the wrong session.
//
// omp's store puts something beside each transcript that pi's has nothing like:
// a directory of per-tool log files, named for the same start timestamp and
// session id, so the id matches the directory's name as well as the
// transcript's. Three of the six transcripts in the real directory have one —
// only agent sessions that ran a tool — which is enough to make the ambiguity
// real without being universal.
//
// Two things keep them out, and only one of them is the suffix. The log files
// sit two levels below sessions/ and this pattern allows exactly one, so depth
// excludes the files whatever they are named; the ".jsonl" suffix is what
// excludes the directory itself. Either way they are out of scope: the
// transcript is the source and the logs are not an interface.
func uniqueTranscript(home, id string) (string, error) {
	if !isSessionID(id) {
		return "", ErrNoSession
	}
	// The two wildcards keep the pattern anchored to one directory below
	// sessions/ and to the end of the file name, so a partial segment cannot
	// match.
	matches, err := filepath.Glob(filepath.Join(home, "sessions", "*", "*"+id+".jsonl"))
	if err != nil {
		return "", fmt.Errorf("find omp transcripts: %w", err)
	}
	if len(matches) != 1 {
		return "", ErrNoSession
	}
	return matches[0], nil
}

// isSessionID rejects anything that cannot be an omp session id. A real one is a
// UUID; a value carrying a glob metacharacter or a path separator would instead
// widen or escape the pattern above, so it is refused before it is interpolated.
func isSessionID(id string) bool {
	return id != "" && !strings.ContainsAny(id, `*?[]\/`)
}
