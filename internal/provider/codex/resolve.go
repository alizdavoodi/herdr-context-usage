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
)

type ResolutionMode string

const (
	ResolutionAgentSession  ResolutionMode = "agent_session"
	ResolutionForegroundCWD ResolutionMode = "foreground_cwd"
)

var ErrNoSession = errors.New("codex agent session cannot be resolved")

type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type SessionInput struct {
	CodexHome     string
	AgentSession  *AgentSession
	ForegroundCWD string
}

type ResolvedSession struct {
	Path string
	Mode ResolutionMode
}

type sessionMetadata struct {
	ID             string          `json:"id"`
	CWD            string          `json:"cwd"`
	ParentThreadID *string         `json:"parent_thread_id"`
	ThreadSource   string          `json:"thread_source"`
	Originator     string          `json:"originator"`
	Source         json.RawMessage `json:"source"`
}

func Resolve(input SessionInput) (ResolvedSession, error) {
	home, err := resolveHome(input.CodexHome)
	if err != nil {
		return ResolvedSession{}, err
	}
	if input.AgentSession != nil && input.AgentSession.Value != "" {
		path, err := reportedPath(home, *input.AgentSession)
		if err != nil {
			return ResolvedSession{}, err
		}
		return ResolvedSession{Path: path, Mode: ResolutionAgentSession}, nil
	}
	if input.ForegroundCWD == "" {
		return ResolvedSession{}, ErrNoSession
	}

	active, err := activeSessionIDs(home)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ResolvedSession{}, ErrNoSession
		}
		return ResolvedSession{}, err
	}
	path, err := rolloutForCWD(home, input.ForegroundCWD, active)
	if err != nil {
		return ResolvedSession{}, err
	}
	return ResolvedSession{Path: path, Mode: ResolutionForegroundCWD}, nil
}

func resolveHome(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if configured := os.Getenv("CODEX_HOME"); configured != "" {
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

func reportedPath(home string, session AgentSession) (string, error) {
	switch strings.ToLower(session.Kind) {
	case "path":
		return session.Value, nil
	case "id":
		paths, err := rolloutPaths(home, session.Value)
		if err != nil {
			return "", err
		}
		if len(paths) == 1 {
			return paths[0], nil
		}
	}
	if filepath.IsAbs(session.Value) {
		return session.Value, nil
	}
	return "", ErrNoSession
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
