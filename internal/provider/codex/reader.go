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
		return 0, ErrNoOccupancy
	}
	if latest.TotalTokens != nil && *latest.TotalTokens > 0 {
		return *latest.TotalTokens, nil
	}
	if latest.InputTokens != nil && *latest.InputTokens > 0 {
		return *latest.InputTokens, nil
	}
	return 0, ErrNoOccupancy
}

func isCompaction(entry rolloutEntry) bool {
	return isCompactionType(entry.Type) || isCompactionType(entry.Payload.Type)
}

func isCompactionType(kind string) bool {
	return strings.EqualFold(kind, "compacted") || strings.EqualFold(kind, "context_compacted")
}
