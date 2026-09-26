package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

var ErrNoSession = errors.New("codex agent session cannot be resolved")

type sessionInput struct {
	CodexHome     string
	AgentSession  *provider.AgentSession
	ForegroundCWD string
}

type resolvedSession struct {
	Path string
	Mode provider.ResolutionMode
}

type sessionMetadata struct {
	ID             string          `json:"id"`
	CWD            string          `json:"cwd"`
	ParentThreadID *string         `json:"parent_thread_id"`
	ThreadSource   string          `json:"thread_source"`
	Originator     string          `json:"originator"`
	Source         json.RawMessage `json:"source"`
}

func resolve(input sessionInput) (resolvedSession, error) {
	if path, ok := input.AgentSession.Path(); ok {
		return resolvedSession{Path: path, Mode: provider.ResolutionAgentSession}, nil
	}
	// Everything below needs the store and a reported path does not, so the
	// home is resolved only once the path branch has passed: looking it up
	// early would let a failing home lookup leave a pane unmeasured for a
	// directory it never needed.
	home, err := provider.Home(input.CodexHome, ".codex")
	if err != nil {
		return resolvedSession{}, err
	}
	if input.AgentSession != nil && input.AgentSession.Value != "" {
		// herdr named a session for this pane, so one this provider cannot use
		// blanks the row rather than falling back: a working-directory match
		// could only disagree with it.
		id, ok := input.AgentSession.ID()
		if !ok {
			return resolvedSession{}, ErrNoSession
		}
		path, err := rolloutForID(home, id)
		if err != nil {
			return resolvedSession{}, err
		}
		return resolvedSession{Path: path, Mode: provider.ResolutionAgentSession}, nil
	}
	if input.ForegroundCWD == "" {
		return resolvedSession{}, ErrNoSession
	}

	active, err := activeSessionIDs(home)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return resolvedSession{}, ErrNoSession
		}
		return resolvedSession{}, err
	}
	path, err := rolloutForCWD(home, input.ForegroundCWD, active)
	if err != nil {
		return resolvedSession{}, err
	}
	return resolvedSession{Path: path, Mode: provider.ResolutionForegroundCWD}, nil
}

// rolloutForID finds the one rollout whose metadata carries id. A match that is
// not unique is unresolved rather than chosen between, and a missing match is
// never rescued by opening the id as a path.
func rolloutForID(home, id string) (string, error) {
	paths, err := rolloutPaths(home, id)
	if err != nil {
		return "", err
	}
	if len(paths) != 1 {
		return "", ErrNoSession
	}
	return paths[0], nil
}

func activeSessionIDs(home string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(filepath.Join(home, "thread-writer-locks"))
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".lock") || strings.HasPrefix(name, ".") {
			continue
		}
		ids[strings.TrimSuffix(name, ".lock")] = struct{}{}
	}
	return ids, nil
}

func rolloutForCWD(home, cwd string, active map[string]struct{}) (string, error) {
	var matches []string
	var candidates sessionCandidates
	err := walkRollouts(home, func(path string, metadata sessionMetadata) {
		if _, ok := active[metadata.ID]; !ok || metadata.CWD != cwd {
			return
		}
		candidates.add(metadata)
		if metadata.isTUIMainThread() {
			matches = append(matches, path)
		}
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", fmt.Errorf("%w: %s", ErrNoSession, candidates.reason(len(matches)))
}

func rolloutPaths(home, sessionID string) ([]string, error) {
	var paths []string
	err := walkRollouts(home, func(path string, metadata sessionMetadata) {
		if metadata.ID == sessionID {
			paths = append(paths, path)
		}
	})
	return paths, err
}

func walkRollouts(home string, visit func(string, sessionMetadata)) error {
	sessions := filepath.Join(home, "sessions")
	// Foreground-CWD correlation must inspect active session metadata instead of
	// guessing from recency. This scans the lifetime corpus on each event, a
	// deliberate CPU trade-off for avoiding stale-session attribution.
	return filepath.WalkDir(sessions, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		metadata, found, err := readSessionMetadata(path)
		if err != nil {
			// An unreadable unrelated rollout cannot make another pane retain a
			// superseded value indefinitely.
			return nil
		}
		if !found {
			return nil
		}
		visit(path, metadata)
		return nil
	})
}

type sessionCandidates struct {
	tui      int
	exec     int
	subagent int
	other    int
}

func (c *sessionCandidates) add(metadata sessionMetadata) {
	switch {
	case metadata.isSubagent():
		c.subagent++
	case metadata.isTUIMainThread():
		c.tui++
	case metadata.isExecMainThread():
		c.exec++
	default:
		c.other++
	}
}

func (c sessionCandidates) reason(tuiMatches int) string {
	if tuiMatches > 1 {
		return "multiple active Codex TUI main threads match foreground cwd"
	}
	if c.exec > 0 && c.tui == 0 {
		return "active Codex rollout matches foreground cwd but is an exec main thread"
	}
	if c.subagent > 0 && c.tui == 0 {
		return "only active Codex subagent rollouts match foreground cwd"
	}
	if c.other > 0 && c.tui == 0 {
		return "active Codex rollout has unrecognised origin metadata"
	}
	return "no active Codex rollout matches foreground cwd"
}

func (metadata sessionMetadata) isTUIMainThread() bool {
	return !metadata.isSubagent() && isTUIOriginator(metadata.Originator) && metadata.sourceName() == "cli"
}

func isTUIOriginator(originator string) bool {
	return strings.EqualFold(originator, "codex-tui") || strings.EqualFold(originator, "codex_cli_rs")
}

func (metadata sessionMetadata) isExecMainThread() bool {
	return !metadata.isSubagent() && (strings.Contains(strings.ToLower(metadata.Originator), "exec") || metadata.sourceName() == "exec")
}

func (metadata sessionMetadata) isSubagent() bool {
	if metadata.ParentThreadID != nil || strings.EqualFold(metadata.ThreadSource, "subagent") {
		return true
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(metadata.Source, &source) != nil {
		return false
	}
	_, ok := source["subagent"]
	return ok
}

func (metadata sessionMetadata) sourceName() string {
	var source string
	if json.Unmarshal(metadata.Source, &source) != nil {
		return ""
	}
	return strings.ToLower(source)
}

func readSessionMetadata(path string) (sessionMetadata, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return sessionMetadata{}, false, err
	}
	defer file.Close()

	var entry struct {
		Type    string          `json:"type"`
		Payload sessionMetadata `json:"payload"`
	}
	if err := json.NewDecoder(bufio.NewReader(file)).Decode(&entry); err != nil {
		return sessionMetadata{}, false, nil
	}
	if entry.Type != "session_meta" || entry.Payload.ID == "" {
		return sessionMetadata{}, false, nil
	}
	return entry.Payload, true, nil
}
