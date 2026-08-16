package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type ResolutionMode string

const (
	ResolutionAgentSession    ResolutionMode = "agent_session"
	ResolutionProcessRegistry ResolutionMode = "process_registry"
	ResolutionForegroundCWD   ResolutionMode = "foreground_cwd"
)

var ErrNoSession = errors.New("claude agent session cannot be resolved")

type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type SessionInput struct {
	ClaudeHome    string
	AgentSession  *AgentSession
	ForegroundCWD string
	ProcessIDs    []int
}

type ResolvedSession struct {
	Path string
	Mode ResolutionMode
}

type processSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Kind      string `json:"kind"`
}

func Resolve(input SessionInput) (ResolvedSession, error) {
	home, err := resolveHome(input.ClaudeHome)
	if err != nil {
		return ResolvedSession{}, err
	}

	if input.AgentSession != nil && input.AgentSession.Value != "" {
		path, err := reportedPath(home, input.ForegroundCWD, *input.AgentSession)
		if err != nil {
			return ResolvedSession{}, err
		}
		return ResolvedSession{Path: path, Mode: ResolutionAgentSession}, nil
	}

	if input.ForegroundCWD == "" {
		return ResolvedSession{}, ErrNoSession
	}
	for _, pid := range input.ProcessIDs {
		session, err := readProcessSession(home, pid)
		if err != nil {
			continue
		}
		if session.PID == pid && session.CWD == input.ForegroundCWD && session.SessionID != "" && (session.Kind == "" || session.Kind == "interactive") {
			return ResolvedSession{
				Path: transcriptPath(home, input.ForegroundCWD, session.SessionID),
				Mode: ResolutionProcessRegistry,
			}, nil
		}
	}

	projectDir := filepath.Join(home, "projects", encodeProjectPath(input.ForegroundCWD))
	matches, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	if err != nil {
		return ResolvedSession{}, fmt.Errorf("find claude transcripts: %w", err)
	}
	if len(matches) == 1 {
		return ResolvedSession{Path: matches[0], Mode: ResolutionForegroundCWD}, nil
	}
	return ResolvedSession{}, ErrNoSession
}

func resolveHome(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if configured := os.Getenv("CLAUDE_CONFIG_DIR"); configured != "" {
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

func reportedPath(home, cwd string, session AgentSession) (string, error) {
	switch strings.ToLower(session.Kind) {
	case "path":
		return session.Value, nil
	case "id":
		if cwd == "" {
			return "", ErrNoSession
		}
		return transcriptPath(home, cwd, session.Value), nil
	default:
		if filepath.IsAbs(session.Value) {
			return session.Value, nil
		}
		return "", ErrNoSession
	}
}

func readProcessSession(home string, pid int) (processSession, error) {
	data, err := os.ReadFile(filepath.Join(home, "sessions", fmt.Sprintf("%d.json", pid)))
	if err != nil {
		return processSession{}, err
	}
	var session processSession
	if err := json.Unmarshal(data, &session); err != nil {
		return processSession{}, err
	}
	return session, nil
}

func transcriptPath(home, cwd, sessionID string) string {
	return filepath.Join(home, "projects", encodeProjectPath(cwd), sessionID+".jsonl")
}

func encodeProjectPath(path string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, filepath.Clean(path))
}
