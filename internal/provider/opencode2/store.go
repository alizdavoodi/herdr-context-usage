package opencode2

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	terminalTitlePrefix = "OC | "
	untitledWindowTitle = "OpenCode"
)

type ResolutionMode string

const ResolutionTerminalTitle ResolutionMode = "terminal_title"

var (
	ErrNoSession     = errors.New("opencode2 agent session cannot be resolved")
	ErrUntitled      = errors.New("opencode2 agent session is untitled")
	ErrNoOccupancy   = errors.New("opencode2 agent session has no current context occupancy")
	ErrStoreNotReady = errors.New("opencode2 store has not been written yet")
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

func Open(ctx context.Context, explicitPath string) (*Store, error) {
	path, err := databasePath(explicitPath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrStoreNotReady
		}
		return nil, fmt.Errorf("stat opencode2 store: %w", err)
	}

	dsn := &url.URL{Scheme: "file", Path: path}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	dsn.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open opencode2 store: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open opencode2 store read-only: %w", err)
	}
	var tableCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema
		WHERE type = 'table' AND name IN ('session_v2', 'session_message')`).Scan(&tableCount); err != nil {
		db.Close()
		return nil, fmt.Errorf("inspect opencode2 store schema: %w", err)
	}
	if tableCount != 2 {
		// An empty SQLite file is a semantic not-ready state, not a transient read failure.
		db.Close()
		return nil, ErrStoreNotReady
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Resolve(ctx context.Context, directory, terminalTitle string) (ResolvedSession, error) {
	if terminalTitle == untitledWindowTitle {
		return ResolvedSession{}, ErrUntitled
	}
	if directory == "" || !strings.HasPrefix(terminalTitle, terminalTitlePrefix) {
		return ResolvedSession{}, ErrNoSession
	}

	title := strings.TrimPrefix(terminalTitle, terminalTitlePrefix)
	if title == "" {
		return ResolvedSession{}, ErrNoSession
	}

	comparison := "title = ?"
	if strings.HasSuffix(title, "...") {
		title = strings.TrimSuffix(title, "...")
		if title == "" {
			return ResolvedSession{}, ErrNoSession
		}
		comparison = "substr(title, 1, length(?)) = ?"
	}

	query := `SELECT id FROM session_v2
		WHERE directory = ? AND parent_id IS NULL AND ` + comparison + `
		LIMIT 2`
	args := []any{directory, title}
	if strings.HasPrefix(comparison, "substr") {
		args = append(args, title)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ResolvedSession{}, fmt.Errorf("resolve opencode2 session: %w", err)
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ResolvedSession{}, fmt.Errorf("read resolved opencode2 session: %w", err)
		}
		matches = append(matches, id)
	}
	if err := rows.Err(); err != nil {
		return ResolvedSession{}, fmt.Errorf("resolve opencode2 session: %w", err)
	}
	if len(matches) != 1 {
		return ResolvedSession{}, ErrNoSession
	}
	return ResolvedSession{ID: matches[0], Mode: ResolutionTerminalTitle}, nil
}

func (s *Store) ReadOccupancy(ctx context.Context, sessionID string) (Reading, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM session_message
		WHERE session_id = ? AND type = 'assistant'
		ORDER BY seq DESC
		LIMIT 1`, sessionID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Reading{}, ErrNoOccupancy
	}
	if err != nil {
		return Reading{}, fmt.Errorf("read opencode2 assistant message: %w", err)
	}

	var payload struct {
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
	if err := json.Unmarshal(data, &payload); err != nil {
		return Reading{}, fmt.Errorf("%w: decode assistant data: %v", ErrNoOccupancy, err)
	}
	if payload.Tokens == nil || payload.Tokens.Input == nil || payload.Tokens.Cache == nil ||
		payload.Tokens.Cache.Read == nil || payload.Tokens.Cache.Write == nil {
		return Reading{}, ErrNoOccupancy
	}

	input := *payload.Tokens.Input
	cacheRead := *payload.Tokens.Cache.Read
	cacheWrite := *payload.Tokens.Cache.Write
	if input < 0 || cacheRead < 0 || cacheWrite < 0 || input > math.MaxInt64-cacheRead || input+cacheRead > math.MaxInt64-cacheWrite {
		return Reading{}, ErrNoOccupancy
	}
	return Reading{
		Occupancy: input + cacheRead + cacheWrite,
		ModelID:   payload.Model.ID,
	}, nil
}

func databasePath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "opencode", "opencode2.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode2.db"), nil
}
