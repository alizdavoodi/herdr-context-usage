package opencode2

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"

	_ "modernc.org/sqlite"
)

const occupancyScanLimit = 256

type ResolutionMode string

// Both opencode providers record the same mode for a title-resolved session.
const ResolutionTerminalTitle ResolutionMode = opencodeshared.ResolutionTerminalTitle

var (
	ErrNoSession     = errors.New("opencode2 agent session cannot be resolved")
	ErrNoOccupancy   = errors.New("opencode2 agent session has no current context occupancy")
	ErrStoreNotReady = errors.New("opencode2 store has not been written yet")
	ErrJustCompacted = errors.New("opencode2 agent session was just compacted")
)

type Store struct {
	db *sql.DB
}

type ResolvedSession struct {
	ID   string
	Mode ResolutionMode
}

type Reading struct {
	Occupancy int64
	ModelID   string
}

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

func Open(ctx context.Context, explicitPath, dataHome string) (*Store, error) {
	path, err := opencodeshared.DataPath(explicitPath, dataHome, "opencode2.db")
	if err != nil {
		return nil, err
	}
	db, err := opencodeshared.OpenReadOnly(ctx, path, "session_v2", "session_message")
	// A database without these tables is one opencode2 has not initialised yet,
	// which for this provider is the same lifecycle state as no file at all.
	if errors.Is(err, opencodeshared.ErrMissing) || errors.Is(err, opencodeshared.ErrSchema) {
		return nil, ErrStoreNotReady
	}
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Resolve(ctx context.Context, directory, terminalTitle string) (ResolvedSession, error) {
	id, err := opencodeshared.ResolveTitle(ctx, directory, terminalTitle, s.sessionsByTitle)
	if err != nil {
		return ResolvedSession{}, err
	}
	return ResolvedSession{ID: id, Mode: ResolutionTerminalTitle}, nil
}

// sessionsByTitle is this store's own half of title correlation. The v2 schema
// keeps root sessions in `session_v2`, and a v2 plugin route interpolates its
// plugin name into the same title shape v1 fills with a plugin id, so only a
// positive root-session match here can produce a reading.
func (s *Store) sessionsByTitle(ctx context.Context, directory string, title opencodeshared.Title) ([]string, error) {
	comparison, args := title.Comparison()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM session_v2 WHERE directory = ? AND parent_id IS NULL AND `+comparison+` LIMIT 2`,
		append([]any{directory}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("resolve opencode2 session: %w", err)
	}
	defer rows.Close()
	return opencodeshared.CollectIDs(rows)
}

func (s *Store) ReadOccupancy(ctx context.Context, sessionID string) (Reading, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT type, data FROM session_message
		WHERE session_id = ? AND type IN ('assistant', 'compaction')
		ORDER BY seq DESC
		LIMIT ?`, sessionID, occupancyScanLimit)
	if err != nil {
		return Reading{}, fmt.Errorf("read opencode2 session messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var messageType string
		var data []byte
		if err := rows.Scan(&messageType, &data); err != nil {
			return Reading{}, fmt.Errorf("read opencode2 session message data: %w", err)
		}
		if messageType == "compaction" {
			// In-flight assistant rows are safe to cross because prior usage stays
			// a lower bound. A completed or running compaction is a hard boundary;
			// a failed attempt changed no context, so its prior usage remains valid.
			var compaction struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(data, &compaction) == nil && compaction.Status == "completed" {
				return Reading{}, ErrJustCompacted
			}
			if compaction.Status == "failed" {
				continue
			}
			return Reading{}, ErrNoOccupancy
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

		input := *payload.Tokens.Input
		cacheRead := *payload.Tokens.Cache.Read
		cacheWrite := *payload.Tokens.Cache.Write
		if input < 0 || cacheRead < 0 || cacheWrite < 0 || input > math.MaxInt64-cacheRead || input+cacheRead > math.MaxInt64-cacheWrite {
			continue
		}
		occupancy := input + cacheRead + cacheWrite
		if occupancy == 0 {
			continue
		}
		return Reading{Occupancy: occupancy, ModelID: payload.Model.ID}, nil
	}
	if err := rows.Err(); err != nil {
		return Reading{}, fmt.Errorf("read opencode2 session messages: %w", err)
	}
	return Reading{}, ErrNoOccupancy
}
