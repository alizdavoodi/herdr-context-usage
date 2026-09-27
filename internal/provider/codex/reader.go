package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

var (
	ErrNoOccupancy   = errors.New("codex rollout has no current context occupancy")
	ErrFailedTurn    = errors.New("codex turn recorded no context occupancy")
	ErrJustCompacted = fmt.Errorf("codex %w", provider.ErrJustCompacted)
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

func readOccupancy(path string) (int64, error) {
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
					// Only a usage row that yields a number ends the just-compacted
					// window. A turn that recorded nothing leaves the previous reading
					// standing as a lower bound, but after a compaction the previous
					// reading is not a bound on anything: it is the pre-compaction
					// number, which is where 245,400 gets displayed for a context that
					// now holds 18,162.
					if _, ok := occupancy(latest); ok {
						justCompacted = false
					}
				} else if strings.EqualFold(entry.Type, "compacted") || strings.EqualFold(entry.Payload.Type, "compacted") {
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
	if value, ok := occupancy(latest); ok {
		return value, nil
	}
	// An all-zero usage row outside a compaction is a turn that recorded
	// nothing, not an empty window. The other providers walk back to the
	// previous reading here; codex reads only the newest row, so it says so
	// instead, and leaving this answer out of Superseded is what lets the value
	// already on the pane stand: occupancy only rises within a turn, so the
	// previous reading is a valid lower bound. No rollout in the local corpus
	// has produced this row — 0 of 7,143 usage events — which is why it needs
	// its own name: one error covering both this and "nothing recorded yet"
	// makes the difference between clearing and preserving invisible.
	return 0, ErrFailedTurn
}

// occupancy reads the one number a usage row can contribute, and reports
// whether it has one at all.
//
// total_tokens is preferred over input_tokens because a compaction zeroes the
// whole breakdown and carries the true post-compaction size only in the total:
// reading input_tokens there yields 0, and refusing that zero and walking back
// yields the pre-compaction number. Both are wrong. Preferring the total costs
// one turn's output tokens, measured at 0.9%.
func occupancy(usage *tokenUsage) (int64, bool) {
	if usage.TotalTokens != nil && *usage.TotalTokens > 0 {
		return *usage.TotalTokens, true
	}
	if usage.InputTokens != nil && *usage.InputTokens > 0 {
		return *usage.InputTokens, true
	}
	return 0, false
}
