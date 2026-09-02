package claude

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider/transcript"
)

// This remains bounded while retaining several consecutive records at the
// observed 600 KiB upper end of Claude tool results.
const maxScanBytes int64 = 8 << 20

var (
	ErrNoOccupancy   = errors.New("claude transcript has no current context occupancy")
	ErrJustCompacted = errors.New("claude agent session is just-compacted")
)

type transcriptEntry struct {
	IsCompactSummary bool `json:"isCompactSummary"`
	Message          *struct {
		Usage *struct {
			InputTokens         int64 `json:"input_tokens"`
			CacheReadTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func ReadOccupancy(path string) (int64, error) {
	// Claude's transcript stamps no schema version, so the head of the scan goes
	// unread here. The bounded tail is the whole of what this reader needs.
	scan, err := transcript.Read("claude", path, maxScanBytes)
	if err != nil {
		return 0, err
	}

	lines := scan.Tail
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var entry transcriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.IsCompactSummary {
			return 0, ErrJustCompacted
		}
		if entry.Message == nil || entry.Message.Usage == nil {
			continue
		}
		usage := entry.Message.Usage
		if usage.InputTokens < 0 || usage.CacheReadTokens < 0 || usage.CacheCreationTokens < 0 {
			continue
		}
		occupancy := usage.InputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
		if occupancy == 0 {
			// An all-zero usage row is a failed turn, not an empty window. Claude
			// writes one on rate-limit and billing errors; opencode v1 writes one on
			// an aborted turn. No store examined has produced a genuine zero for a
			// live session, so treat it as no occupancy rather than as a value.
			// Representing unknown as null rather than zero is a separate rule and
			// still holds — see docs/design.md.
			continue
		}
		return occupancy, nil
	}

	return 0, ErrNoOccupancy
}
