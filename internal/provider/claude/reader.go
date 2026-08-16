package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxScanBytes int64 = 1 << 20

var ErrNoOccupancy = errors.New("claude transcript has no current context occupancy")

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
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open claude transcript: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat claude transcript: %w", err)
	}
	start := max(info.Size()-maxScanBytes, 0)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek claude transcript: %w", err)
	}

	data, err := io.ReadAll(io.LimitReader(file, maxScanBytes))
	if err != nil {
		return 0, fmt.Errorf("read claude transcript tail: %w", err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	if start > 0 && len(lines) > 0 {
		// The first bytes may be the tail of a large JSONL record.
		lines = lines[1:]
	}

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
			return 0, ErrNoOccupancy
		}
		if entry.Message == nil || entry.Message.Usage == nil {
			continue
		}
		usage := entry.Message.Usage
		if usage.InputTokens < 0 || usage.CacheReadTokens < 0 || usage.CacheCreationTokens < 0 {
			continue
		}
		return usage.InputTokens + usage.CacheReadTokens + usage.CacheCreationTokens, nil
	}

	return 0, ErrNoOccupancy
}
