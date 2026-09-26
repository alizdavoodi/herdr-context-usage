// Package opencodeshared holds what the two opencode providers genuinely have
// in common. Their stores are different databases with different schemas, so
// every query stays in its own provider; the terminal-title format, what makes
// a title unusable, and how a missing store is classified are identical and
// live here.
package opencodeshared

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	titlePrefix   = "OC | "
	untitledTitle = "OpenCode"

	// A session title longer than 40 characters is written as
	// slice(0,37)+"...", so 40 is the longest a session title can be on the
	// wire. Read from the TUI of opencode 1.18.18 and opencode2
	// 0.0.0-next-17444, which agree on the format, the limit and the three
	// title forms; only the interpolated field differs, and that lives in each
	// provider's own query rather than here.
	maxSessionTitle = 40
	truncationMark  = "..."

	// opencode reads this from its own environment. We read it from ours, which
	// is the same environment whenever herdr was started from a shell that
	// exports it — the common case, and the only channel available to us.
	// opencode 1.18.18 accepts "true" and "1" case-insensitively and nothing
	// else; opencode2 0.0.0-next-17444 does not read this variable at all.
	disableTitleEnv = "OPENCODE_DISABLE_TERMINAL_TITLE"
)

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

// Title is a pane's terminal title, once it has been established that it names
// an agent session.
type Title struct {
	// Text is the session title as the TUI wrote it, without the truncation
	// mark. It is a prefix of the stored title when Truncated is set.
	Text string
	// Truncated records that the TUI shortened the title, so only a prefix of
	// the stored title can be compared.
	Truncated bool
}

// Comparison returns the SQL predicate that matches this title against a
// sessions table, and its arguments. The predicate travels between the two
// providers; the query it is spliced into does not, because the two stores keep
// root sessions in different tables.
func (t Title) Comparison() (string, []any) {
	if t.Truncated {
		return "substr(title, 1, length(?)) = ?", []any{t.Text, t.Text}
	}
	return "title = ?", []any{t.Text}
}

// Lookup is a provider's own query for the sessions a title could name. It
// returns at most two ids, because one is a match and two is an ambiguity.
type Lookup func(ctx context.Context, directory string, title Title) ([]string, error)

// CollectIDs drains a lookup's rows. It is shared so that a query returning two
// rows means the same thing in both providers.
func CollectIDs(rows *sql.Rows) ([]string, error) {
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read resolved opencode session: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve opencode session: %w", err)
	}
	return ids, nil
}

// parseTitle decides which of the three title forms a pane is showing.
func parseTitle(terminalTitle string) (Title, error) {
	if terminalTitle == untitledTitle {
		return Title{}, ErrUntitled
	}
	text, ok := strings.CutPrefix(terminalTitle, titlePrefix)
	if !ok || text == "" {
		if titleDisabled() {
			return Title{}, ErrTitleDisabled
		}
		return Title{}, ErrTitleUnavailable
	}
	if prefix, truncated := strings.CutSuffix(text, truncationMark); truncated {
		if prefix == "" {
			return Title{}, ErrTitleUnavailable
		}
		return Title{Text: prefix, Truncated: true}, nil
	}
	// Only a session title is ever shortened, so a title too long to have
	// survived shortening is a plugin's. Counted in runes because the TUI counts
	// UTF-16 units: the two agree for every title short enough to matter, and
	// where they disagree this one is the more permissive, which keeps a real
	// session title from being dismissed as a plugin's.
	if utf8.RuneCountInString(text) > maxSessionTitle {
		return Title{}, ErrPluginTitle
	}
	return Title{Text: text}, nil
}

// ResolveTitle turns a pane's directory and terminal title into exactly one
// agent session id. Both providers share every step except the lookup, which
// they own because their schemas differ.
func ResolveTitle(ctx context.Context, directory, terminalTitle string, lookup Lookup) (string, error) {
	title, err := parseTitle(terminalTitle)
	if err != nil {
		return "", err
	}
	if directory == "" {
		// Titles are not unique across directories, so without one there is
		// nothing to disambiguate with and recency is banned.
		return "", ErrNoTitleMatch
	}
	matches, err := lookup(ctx, directory, title)
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", ErrNoTitleMatch
	}
	return matches[0], nil
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
