package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

type assistantMessage struct {
	Role   string       `json:"role"`
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

type Store struct {
	database *sql.DB
}

func DatabasePath(dataDir string) (string, error) {
	if dataDir != "" {
		return filepath.Join(dataDir, "opencode.db"), nil
	}
	if dataDir = os.Getenv("XDG_DATA_HOME"); dataDir != "" {
		return filepath.Join(dataDir, "opencode", "opencode.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db"), nil
}

func Open(ctx context.Context, dataDir string) (*Store, error) {
	path, err := DatabasePath(dataDir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && info.IsDir()) {
		return nil, ErrStoreNotReady
	}
	if err != nil {
		return nil, fmt.Errorf("stat opencode database: %w", err)
	}

	database, err := openReadOnly(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open opencode database: %w", err)
	}
	if err := hasV1Schema(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	return &Store{database: database}, nil
}

func (s *Store) Close() error {
	return s.database.Close()
}

func (s *Store) ResolveTerminalTitle(ctx context.Context, directory, terminalTitle string) (ResolvedSession, error) {
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
	args := []any{directory, title}
	if strings.HasSuffix(title, "...") {
		title = strings.TrimSuffix(title, "...")
		if title == "" {
			return ResolvedSession{}, ErrNoSession
		}
		comparison = "substr(title, 1, length(?)) = ?"
		args = []any{directory, title, title}
	}
	rows, err := s.database.QueryContext(ctx, `SELECT id FROM session WHERE directory = ? AND `+comparison+` LIMIT 2`, args...)
	if err != nil {
		return ResolvedSession{}, fmt.Errorf("resolve opencode session: %w", err)
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ResolvedSession{}, fmt.Errorf("read resolved opencode session: %w", err)
		}
		matches = append(matches, id)
	}
	if err := rows.Err(); err != nil {
		return ResolvedSession{}, fmt.Errorf("resolve opencode session: %w", err)
	}
	if len(matches) != 1 {
		return ResolvedSession{}, ErrNoSession
	}
	return ResolvedSession{ID: matches[0], Mode: ResolutionTerminalTitle}, nil
}

func (s *Store) ReadOccupancy(ctx context.Context, sessionID string) (int64, error) {
	if err := sessionExists(ctx, s.database, sessionID); err != nil {
		return 0, err
	}

	rows, err := s.database.QueryContext(ctx, `SELECT data FROM message WHERE session_id = ? ORDER BY time_created DESC, id DESC`, sessionID)
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
		if message.Role != "assistant" {
			continue
		}
		return occupancy(message)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate opencode messages: %w", err)
	}
	return 0, ErrNoOccupancy
}

func openReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	dsn := &url.URL{Scheme: "file", Path: path}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	dsn.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func hasV1Schema(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name IN ('session', 'message')`)
	if err != nil {
		return fmt.Errorf("probe opencode schema: %w", err)
	}
	defer rows.Close()

	present := make(map[string]bool, 2)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan opencode schema: %w", err)
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate opencode schema: %w", err)
	}
	if !present["session"] || !present["message"] {
		return ErrNoSchema
	}
	return nil
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
	if message.Tokens == nil || message.Tokens.Input == nil || message.Tokens.Cache == nil || message.Tokens.Cache.Read == nil || message.Tokens.Cache.Write == nil {
		return 0, ErrNoOccupancy
	}
	if *message.Tokens.Input < 0 || *message.Tokens.Cache.Read < 0 || *message.Tokens.Cache.Write < 0 {
		return 0, ErrNoOccupancy
	}
	occupancy := *message.Tokens.Input + *message.Tokens.Cache.Read + *message.Tokens.Cache.Write
	if occupancy == 0 {
		return 0, ErrNoOccupancy
	}
	return occupancy, nil
}
