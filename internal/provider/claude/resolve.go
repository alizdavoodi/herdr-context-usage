package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

type ResolutionMode string

const (
	ResolutionAgentSession    ResolutionMode = "agent_session"
	ResolutionProcessRegistry ResolutionMode = "process_registry"
	ResolutionForegroundCWD   ResolutionMode = "foreground_cwd"
)

var ErrNoSession = errors.New("claude agent session cannot be resolved")

type SessionInput struct {
	ClaudeHome    string
	AgentSession  *provider.AgentSession
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
	if path, ok := input.AgentSession.Path(); ok {
		return ResolvedSession{Path: path, Mode: ResolutionAgentSession}, nil
	}

	// Everything below needs the store and a reported path does not, so the
	// home is resolved only once the path branch has passed: looking it up
	// early would let a failing home lookup leave a pane unmeasured for a
	// directory it never needed.
	home, err := provider.Home(input.ClaudeHome, ".claude")
	if err != nil {
		return ResolvedSession{}, err
	}

	if input.AgentSession != nil && input.AgentSession.Value != "" {
		// herdr named a session for this pane, so one this provider cannot use
		// blanks the row rather than falling back: a working-directory match
		// could only disagree with it.
		id, ok := input.AgentSession.ID()
		if !ok || !isSessionID(id) || input.ForegroundCWD == "" {
			return ResolvedSession{}, ErrNoSession
		}
		return ResolvedSession{Path: transcriptPath(home, input.ForegroundCWD, id), Mode: ResolutionAgentSession}, nil
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

// isSessionID rejects anything that cannot be a claude session id before it is
// joined into a path. A real one is a UUID; a value carrying a path separator
// would build a path outside the pane's own project directory and could name
// another project's transcript, which is a wrong number rather than a blank.
// Unlike codex's rollouts or pi's transcripts, nothing here is matched against
// the store, so this is the only thing standing between the id and the path.
func isSessionID(id string) bool {
	return !strings.ContainsAny(id, `/\`)
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
