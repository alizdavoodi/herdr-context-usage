// Package opencodeshared holds what the two opencode providers genuinely have
// in common: where their stores live, the terminal-title format, what makes a
// title unusable, and the query that turns a title into a root session,
// parameterised by the table each store keeps root sessions in. Their message
// queries stay in each provider, because the two stores have different schemas.
package opencodeshared

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	titlePrefix   = "OC | "
	untitledTitle = "OpenCode"

	// A session title longer than 40 characters is written as slice(0,37) plus
	// a truncation mark, so 40 is the longest a session title can be on the
	// wire. Read from the TUI of opencode 1.18.18 and opencode2
	// 0.0.0-next-17444, which agree on the format, the limit and the three
	// title forms; only the interpolated field differs, which each provider notes
	// where it names its sessions table. opencode 1.18.32 and opencode2 2.0.18
	// keep all of that and change only the mark.
	maxSessionTitle = 40
	// truncatedTitle is the length the TUI cuts to, in the UTF-16 units its
	// slice counts, which is the only length a mark follows.
	truncatedTitle = 37

	// opencode reads this from its own environment. We read it from ours, which
	// is the same environment whenever herdr was started from a shell that
	// exports it — the common case, and the only channel available to us.
	// opencode 1.18.18 accepts "true" and "1" case-insensitively and nothing
	// else; opencode2 0.0.0-next-17444 does not read this variable at all.
	disableTitleEnv = "OPENCODE_DISABLE_TERMINAL_TITLE"
)

// truncationMarks are both marks a TUI has shortened a title with: three dots
// in opencode 1.18.18 and opencode2 0.0.0-next-17444, and U+2026 by opencode
// 1.18.32 and opencode2 2.0.18, the earliest builds checked. A mark
// the parser does not know makes the title read as whole, and a whole title
// that was shortened matches no session.
var truncationMarks = []string{"...", "…"}

var (
	// ErrUntitled is the TUI showing its bare name: its home screen, or a
	// session that has no title yet. Around 10% of sessions sit here.
	ErrUntitled = errors.New("opencode agent session is untitled")

	// ErrTitleUnavailable is a pane whose title is not an opencode title at
	// all, which is also what a pane looks like when title reporting is off.
	ErrTitleUnavailable = errors.New("opencode pane reports no agent session title")

	// ErrTitleDisabled is ErrTitleUnavailable with the reason attached, so a
	// permanently blank row is diagnosable from the plugin's own output rather
	// than only from the user's shell profile.
	ErrTitleDisabled = fmt.Errorf("%w: %s is set, which turns the terminal title off", ErrTitleUnavailable, disableTitleEnv)

	// ErrPluginTitle is a plugin route's title. It shares the session title's
	// shape and identifies no session, so blanking is correct — recognising it
	// is what makes that blank explainable rather than mysterious.
	ErrPluginTitle = errors.New("opencode terminal title names a plugin route rather than an agent session")

	// ErrNoTitleMatch is a well-formed session title that no single root
	// session answers to. Ambiguity lands here too: with 14 sessions sharing one
	// repository directory, a guess is far more likely to be wrong than right.
	ErrNoTitleMatch = errors.New("opencode terminal title matches no single agent session")
)

// parsedTitle is a pane's terminal title, once it has been established that it
// names an agent session.
type parsedTitle struct {
	// Text is the session title as the TUI wrote it, without the truncation
	// mark. It is a prefix of the stored title when Truncated is set.
	Text string
	// Truncated records that the TUI shortened the title, so only a prefix of
	// the stored title can be compared.
	Truncated bool
}

// parseTitle decides which of the three title forms a pane is showing.
func parseTitle(terminalTitle string) (parsedTitle, error) {
	if terminalTitle == untitledTitle {
		return parsedTitle{}, ErrUntitled
	}
	text, ok := strings.CutPrefix(terminalTitle, titlePrefix)
	if !ok || text == "" {
		if titleDisabled() {
			return parsedTitle{}, ErrTitleDisabled
		}
		return parsedTitle{}, ErrTitleUnavailable
	}
	for _, mark := range truncationMarks {
		prefix, marked := strings.CutSuffix(text, mark)
		if marked && prefix == "" {
			return parsedTitle{}, ErrTitleUnavailable
		}
		// A mark after any other length is the title's own text: a short title
		// read as a prefix is ambiguous beside every title it begins, and a long
		// plugin name read as one would slip past the plugin guard below.
		if marked && len(utf16.Encode([]rune(prefix))) == truncatedTitle {
			return parsedTitle{Text: prefix, Truncated: true}, nil
		}
	}
	// Only a session title is ever shortened, so a title too long to have
	// survived shortening is a plugin's. Counted in runes because the TUI counts
	// UTF-16 units: the two agree for every title short enough to matter, and
	// where they disagree this one is the more permissive, which keeps a real
	// session title from being dismissed as a plugin's.
	if utf8.RuneCountInString(text) > maxSessionTitle {
		return parsedTitle{}, ErrPluginTitle
	}
	return parsedTitle{Text: text}, nil
}

// ResolveTitle turns a pane's directory and terminal title into exactly one root
// agent session id from table, the one a store keeps its root sessions in.
func ResolveTitle(ctx context.Context, db *sql.DB, table, directory, terminalTitle string) (string, error) {
	title, err := parseTitle(terminalTitle)
	if err != nil {
		return "", err
	}
	if directory == "" {
		// Titles are not unique across directories, so without one there is
		// nothing to disambiguate with and recency is banned.
		return "", ErrNoTitleMatch
	}
	predicate, args := "title = ?", []any{directory, title.Text}
	if title.Truncated {
		predicate, args = "substr(title, 1, length(?)) = ?", []any{directory, title.Text, title.Text}
	}
	// table only ever comes from the two providers' own constants, never from
	// input, which is what makes splicing it into the query safe. Two rows are
	// enough to decide, because one is a match and two is an ambiguity.
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM `+table+` WHERE directory = ? AND parent_id IS NULL AND `+predicate+` LIMIT 2`, args...)
	if err != nil {
		return "", fmt.Errorf("resolve opencode session: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("read resolved opencode session: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("resolve opencode session: %w", err)
	}
	if len(ids) != 1 {
		return "", ErrNoTitleMatch
	}
	return ids[0], nil
}

// titleDisabled reports whether this process inherited the variable that turns
// opencode's terminal title off. It is a hint about our own environment rather
// than proof about the agent's, which is why it only ever annotates a title we
// already failed to read.
func titleDisabled() bool {
	switch strings.ToLower(os.Getenv(disableTitleEnv)) {
	case "true", "1":
		return true
	}
	return false
}
