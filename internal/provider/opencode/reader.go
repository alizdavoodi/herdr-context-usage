package opencode

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"

	_ "modernc.org/sqlite"
)

type assistantMessage struct {
	Role    string          `json:"role"`
	Summary json.RawMessage `json:"summary"`
	Time    struct {
		Completed *int64 `json:"completed"`
	} `json:"time"`
	Tokens *tokenCounts `json:"tokens"`
}

type tokenCounts struct {
	Input *int64       `json:"input"`
	Cache *cacheCounts `json:"cache"`
}

type cacheCounts struct {
	Read  *int64 `json:"read"`
	Write *int64 `json:"write"`
}

type store struct{ *sql.DB }

func open(ctx context.Context, explicitPath, dataHome string) (*store, error) {
	path, err := opencodeshared.DataPath(explicitPath, dataHome, "opencode.db")
	if err != nil {
		return nil, err
	}
	// opencode's schema is mid-migration: the v1 database also carries a stalled
	// session_v2, so having session and message is what makes a file a v1 store.
	db, err := sqlitestore.OpenReadOnly(ctx, path, "session", "message")
	if err != nil {
		return nil, err
	}
	return &store{DB: db}, nil
}

func (s *store) readOccupancy(ctx context.Context, sessionID string) (int64, error) {
	if err := sessionExists(ctx, s.DB, sessionID); err != nil {
		return 0, err
	}

	rows, err := s.QueryContext(ctx, `SELECT data FROM message WHERE session_id = ? ORDER BY time_created DESC, id DESC`, sessionID)
	if err != nil {
		return 0, fmt.Errorf("read opencode messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return 0, fmt.Errorf("scan opencode message: %w", err)
		}
		var message assistantMessage
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		if isCompactionSummary(message.Summary) {
			return 0, ErrJustCompacted
		}
		if message.Role != "assistant" {
			continue
		}
		occupancy, err := occupancy(message)
		if err != nil {
			continue
		}
		return occupancy, nil
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate opencode messages: %w", err)
	}
	return 0, ErrNoOccupancy
}

func sessionExists(ctx context.Context, database *sql.DB, sessionID string) error {
	var found int
	err := database.QueryRowContext(ctx, `SELECT 1 FROM session WHERE id = ?`, sessionID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSession
	}
	if err != nil {
		return fmt.Errorf("resolve opencode session: %w", err)
	}
	return nil
}

func occupancy(message assistantMessage) (int64, error) {
	if message.Time.Completed == nil || message.Tokens == nil || message.Tokens.Input == nil || message.Tokens.Cache == nil || message.Tokens.Cache.Read == nil || message.Tokens.Cache.Write == nil {
		return 0, ErrNoOccupancy
	}
	occupancy, ok := provider.Occupancy(*message.Tokens.Input, *message.Tokens.Cache.Read, *message.Tokens.Cache.Write)
	if !ok {
		return 0, ErrNoOccupancy
	}
	return occupancy, nil
}

// isCompactionSummary matches the assistant message that wrote a compaction
// summary, and nothing else.
//
// A v1 compaction is `"summary": true` on an assistant message whose mode and
// agent are both "compaction": 233 rows in the store examined, every one that
// shape. A `summary` object is a different field with the same name — 4,910
// rows, every one a user message carrying {"diffs": …} and occasionally a
// title, which opencode attaches to ordinary prompts. Accepting any non-null
// summary matched both, so the row a session sits behind for the whole of
// every turn read as a compaction boundary: replayed against the 40 most
// recently active sessions, that blanked 20 of them mid-turn, 19 of which had
// never compacted.
func isCompactionSummary(summary json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(summary), []byte("true"))
}
