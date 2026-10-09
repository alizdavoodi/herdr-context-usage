package opencode2

import (
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

const occupancyScanLimit = 256

var (
	ErrNoOccupancy   = errors.New("opencode2 agent session has no current context occupancy")
	ErrJustCompacted = fmt.Errorf("opencode2 %w", provider.ErrJustCompacted)
)

type store struct{ *sql.DB }

type assistantPayload struct {
	Time struct {
		Completed *int64 `json:"completed"`
	} `json:"time"`
	Model struct {
		ID string `json:"id"`
	} `json:"model"`
	Tokens *struct {
		Input *int64 `json:"input"`
		Cache *struct {
			Read  *int64 `json:"read"`
			Write *int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

func open(ctx context.Context, explicitPath, dataHome string) (*store, error) {
	path, err := opencodeshared.DataPath(explicitPath, dataHome, "opencode2.db")
	if err != nil {
		return nil, err
	}
	db, err := sqlitestore.OpenReadOnly(ctx, path, "session_v2", "session_message")
	if err != nil {
		return nil, err
	}
	return &store{DB: db}, nil
}

func (s *store) readOccupancy(ctx context.Context, sessionID string) (provider.Reading, error) {
	rows, err := s.QueryContext(ctx, `SELECT type, data FROM session_message
		WHERE session_id = ? AND type IN ('assistant', 'compaction')
		ORDER BY seq DESC
		LIMIT ?`, sessionID, occupancyScanLimit)
	if err != nil {
		return provider.Reading{}, fmt.Errorf("read opencode2 session messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var messageType string
		var data []byte
		if err := rows.Scan(&messageType, &data); err != nil {
			return provider.Reading{}, fmt.Errorf("read opencode2 session message data: %w", err)
		}
		if messageType == "compaction" {
			// In-flight assistant rows are safe to cross because prior usage stays
			// a lower bound. A completed or running compaction is a hard boundary;
			// a failed attempt changed no context, so its prior usage remains valid.
			var compaction struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(data, &compaction) == nil && compaction.Status == "completed" {
				return provider.Reading{}, ErrJustCompacted
			}
			if compaction.Status == "failed" {
				continue
			}
			return provider.Reading{}, ErrNoOccupancy
		}
		var payload assistantPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		if payload.Time.Completed == nil {
			continue
		}
		if payload.Tokens == nil || payload.Tokens.Input == nil || payload.Tokens.Cache == nil ||
			payload.Tokens.Cache.Read == nil || payload.Tokens.Cache.Write == nil {
			continue
		}
		occupancy, ok := provider.Occupancy(*payload.Tokens.Input, *payload.Tokens.Cache.Read, *payload.Tokens.Cache.Write)
		if !ok {
			continue
		}
		return provider.Reading{Occupancy: occupancy, ModelID: payload.Model.ID}, nil
	}
	if err := rows.Err(); err != nil {
		return provider.Reading{}, fmt.Errorf("read opencode2 session messages: %w", err)
	}
	return provider.Reading{}, ErrNoOccupancy
}
