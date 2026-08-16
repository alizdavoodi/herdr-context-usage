package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var (
	ErrNoOccupancy   = errors.New("codex rollout has no current context occupancy")
	ErrFailedTurn    = errors.New("codex turn recorded no context occupancy")
	ErrJustCompacted = errors.New("codex agent session is just-compacted")
)

type rolloutEntry struct {
	Type    string `json:"type"`
	Payload struct {
		Type string `json:"type"`
		Info struct {
			LastTokenUsage *tokenUsage `json:"last_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

type tokenUsage struct {
	InputTokens *int64 `json:"input_tokens"`
	TotalTokens *int64 `json:"total_tokens"`
}

func ReadOccupancy(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open codex rollout: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var latest *tokenUsage
	justCompacted := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var entry rolloutEntry
			if json.Unmarshal(line, &entry) == nil {
				if entry.Payload.Info.LastTokenUsage != nil {
					latest = entry.Payload.Info.LastTokenUsage
					justCompacted = false
				} else if isCompaction(entry) {
					justCompacted = true
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("read codex rollout: %w", err)
		}
	}

	if justCompacted {
		return 0, ErrJustCompacted
	}
	if latest == nil {
		// The session has produced no reading at all, so whatever is displayed
		// belongs to a session this pane has left.
		return 0, ErrNoOccupancy
	}
	if latest.TotalTokens != nil && *latest.TotalTokens > 0 {
		return *latest.TotalTokens, nil
	}
	if latest.InputTokens != nil && *latest.InputTokens > 0 {
		return *latest.InputTokens, nil
	}
	// An all-zero usage row is a turn that recorded nothing, not an empty
	// window. The other three providers walk back to the previous reading here;
	// codex reads only the newest row, so it says so instead and the value
	// already on the pane stands as the lower bound. No rollout in the local
	// corpus has produced this row, which is why it needs its own name: one
	// error covering both this and "nothing recorded yet" makes the difference
	// between clearing and preserving invisible.
	return 0, ErrFailedTurn
}

func isCompaction(entry rolloutEntry) bool {
	return isCompactionType(entry.Type) || isCompactionType(entry.Payload.Type)
}

func isCompactionType(kind string) bool {
	return strings.EqualFold(kind, "compacted")
}
