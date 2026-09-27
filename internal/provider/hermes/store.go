package hermes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

var (
	ErrStoreNotReady = errors.New("hermes store has not been written yet")
	ErrNoOccupancy   = errors.New("hermes agent session has no current context occupancy")
	ErrJustCompacted = errors.New("hermes agent session was just compacted")
	// ErrSessionEnded is a reported session Hermes has since closed. Every
	// resume path clears ended_at again, so a closed row is not the session
	// live in the pane; the desktop, tui and acp platforms keep reporting a
	// session long after it ends, because their integration reports only at
	// start and at /new.
	ErrSessionEnded = errors.New("hermes agent session has ended")
)

type store struct {
	db *sql.DB
}

func open(ctx context.Context, path string) (*store, error) {
	db, err := sqlitestore.OpenReadOnly(ctx, path, "sessions", "messages")
	if errors.Is(err, sqlitestore.ErrMissing) || errors.Is(err, sqlitestore.ErrSchema) {
		return nil, ErrStoreNotReady
	}
	if err != nil {
		return nil, err
	}
	return &store{db: db}, nil
}

func (s *store) Close() error {
	return s.db.Close()
}

// modelConfig is the one key of sessions.model_config this provider reads.
// prompt_tokens is Hermes's own input + cache read + cache write for the main
// loop's latest API call, rewritten after every call, so it is occupancy and
// not session spend. The cumulative token columns beside it are spend and
// messages.token_count is never written, which is why neither is read (ADR-0007).
//
// base_count is how many transcript messages the anchored call priced, read
// only to catch an anchor that has outlived its transcript.
//
// An int64 field lets only an integer pass: Hermes writes an int, and the
// decoder rejects a string or a float, a format this plugin has not seen.
type modelConfig struct {
	UsageAnchor struct {
		PromptTokens int64 `json:"prompt_tokens"`
		BaseCount    int64 `json:"base_count"`
	} `json:"_usage_anchor"`
}

func (s *store) readOccupancy(ctx context.Context, id string) (provider.Reading, error) {
	var reading provider.Reading
	// The version is not a compatibility gate. It is recorded so that a blank
	// row caused by a schema change is diagnosable, which is why it is read
	// before anything that can fail.
	var version int
	if s.db.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&version) == nil {
		reading.TranscriptVersion = &version
	}

	var model, config, endReason sql.NullString
	var ended bool
	err := s.db.QueryRowContext(ctx,
		`SELECT model, model_config, ended_at IS NOT NULL, end_reason FROM sessions WHERE id = ?`, id).
		Scan(&model, &config, &ended, &endReason)
	if errors.Is(err, sql.ErrNoRows) {
		// Hermes creates the row lazily, so a session that has not finished a
		// call yet looks the same as one filed under another home.
		return reading, ErrNoSession
	}
	if err != nil {
		return reading, fmt.Errorf("read hermes session: %w", err)
	}
	if ended {
		// Rotating compaction ends the parent and continues in a child row that
		// the integration reports only at the cli's next call, so this parent is
		// the compaction boundary rather than a closed session.
		if endReason.String == "compression" {
			return reading, ErrJustCompacted
		}
		return reading, ErrSessionEnded
	}

	// Hermes never persists an anchor whose base_count is not positive, so one
	// without it is treated as absent rather than trusted unchecked.
	var decoded modelConfig
	if json.Unmarshal([]byte(config.String), &decoded) == nil &&
		decoded.UsageAnchor.PromptTokens > 0 && decoded.UsageAnchor.BaseCount > 0 {
		// In-place compaction commits the new transcript before it clears the
		// anchor, in a separate write whose failure Hermes swallows, so a stale
		// positive anchor can sit beside a compacted transcript. Hermes's own
		// backstop is that the anchor stops matching once the transcript is
		// shorter than base_count; this mirrors that half of it. Hermes flushes
		// pending messages before each tool runs, and every anchored live session
		// measured had exactly base_count + 1 active rows, so the count falls short
		// only in a sub-second window after an anchor write, which errs to blank.
		// ponytail: a compaction that leaves at least base_count active rows is not
		// caught. The full check is Hermes's base_last_fp fingerprint, rejected
		// because it means byte-matching Python's json.dumps in Go.
		var active int64
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM messages WHERE session_id = ? AND active = 1`, id).Scan(&active); err != nil {
			return reading, fmt.Errorf("read hermes transcript length: %w", err)
		}
		if active < decoded.UsageAnchor.BaseCount {
			return reading, ErrJustCompacted
		}
		reading.Occupancy = decoded.UsageAnchor.PromptTokens
		reading.ModelID = model.String
		return reading, nil
	}

	// In-place compaction, Hermes's default, deletes the anchor and marks the
	// replaced rows compacted, so a compacted row is what tells that boundary
	// apart from a session that has simply not been priced yet.
	// ponytail: an anchor cleared by a later resume in a once-compacted session
	// is labelled just-compacted too. Both answers clear the row, so only the
	// record's context_state differs; a per-compaction marker would split them.
	var compacted bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM messages WHERE session_id = ? AND compacted = 1)`, id).Scan(&compacted); err != nil {
		return reading, fmt.Errorf("read hermes compaction state: %w", err)
	}
	if compacted {
		return reading, ErrJustCompacted
	}
	return reading, ErrNoOccupancy
}
