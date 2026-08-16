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
	ID             string  `json:"id"`
	CWD            string  `json:"cwd"`
	ParentThreadID *string `json:"parent_thread_id"`
	ThreadSource   string  `json:"thread_source"`
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
	if path == "" {
		return ResolvedSession{}, ErrNoSession
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
	err := walkRollouts(home, func(path string, metadata sessionMetadata) {
		if _, ok := active[metadata.ID]; !ok || metadata.CWD != cwd || metadata.ParentThreadID != nil || strings.EqualFold(metadata.ThreadSource, "subagent") {
			return
		}
		matches = append(matches, path)
	})
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", nil
	}
	return matches[0], nil
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
	return filepath.WalkDir(sessions, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		metadata, found, err := readSessionMetadata(path)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		visit(path, metadata)
		return nil
	})
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
