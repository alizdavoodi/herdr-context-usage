package claude

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/transcript"
)

// This remains bounded while retaining several consecutive records at the
// observed 600 KiB upper end of Claude tool results.
const maxScanBytes int64 = 8 << 20

var (
	ErrNoOccupancy   = fmt.Errorf("claude %w", provider.ErrNoOccupancy)
	ErrJustCompacted = fmt.Errorf("claude %w", provider.ErrJustCompacted)
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

func readOccupancy(path string) (int64, error) {
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
		// A row Occupancy rejects is walked past rather than reported, because
		// unknown is null rather than zero — see docs/design.md.
		usage := entry.Message.Usage
		if occupancy, ok := provider.Occupancy(usage.InputTokens, usage.CacheReadTokens, usage.CacheCreationTokens); ok {
			return occupancy, nil
		}
	}

	return 0, ErrNoOccupancy
}
