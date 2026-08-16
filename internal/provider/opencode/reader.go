package opencode

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
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

func ReadOccupancy(databasePath, sessionID string) (int64, error) {
	database, err := openReadOnly(databasePath)
	if err != nil {
		return 0, fmt.Errorf("open opencode database: %w", err)
	}
	defer database.Close()

	if err := hasV1Schema(database); err != nil {
		return 0, err
	}
	if err := sessionExists(database, sessionID); err != nil {
		return 0, err
	}

	rows, err := database.Query(`SELECT data FROM message WHERE session_id = ? ORDER BY time_created DESC, id DESC`, sessionID)
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

func openReadOnly(path string) (*sql.DB, error) {
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func hasV1Schema(database *sql.DB) error {
	rows, err := database.Query(`SELECT name FROM sqlite_schema WHERE type = 'table' AND name IN ('session', 'message')`)
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

func sessionExists(database *sql.DB, sessionID string) error {
	var found int
	err := database.QueryRow(`SELECT 1 FROM session WHERE id = ?`, sessionID).Scan(&found)
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
	return *message.Tokens.Input + *message.Tokens.Cache.Read + *message.Tokens.Cache.Write, nil
}
