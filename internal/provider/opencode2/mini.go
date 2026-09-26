package opencode2

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
)

// miniSubcommand is opencode2's minimal interactive interface. Measured against
// opencode2 0.0.0-next-17444, which is not pinned: it does not set the terminal
// title the ordinary TUI sets, so a pane running it reports whatever the shell
// last wrote and never an "OC | " title.
const miniSubcommand = "mini"

// ErrMiniInterface is ErrTitleUnavailable with the reason attached, the same
// shape ErrTitleDisabled uses, so a permanently blank row becomes diagnosable
// from this plugin's own output.
//
// It takes precedence over ErrTitleDisabled where both could apply. That is not
// arbitrary: this is positive evidence read from the pane's own process list,
// whereas ErrTitleDisabled rests on a variable read from our own environment,
// which opencodeshared hedges as a hint rather than proof about the agent's. A
// mini pane started from a shell exporting OPENCODE_DISABLE_TERMINAL_TITLE would
// otherwise be blamed on the variable, which is a confidently wrong reason for a
// correct blank.
//
// Wrapping ErrTitleUnavailable is also what keeps the classification right
// without touching States: that answer is already superseded, and
// errors.Is reaches through.
var ErrMiniInterface = fmt.Errorf("%w: the pane runs the %s interface, which sets no opencode terminal title",
	opencodeshared.ErrTitleUnavailable, miniSubcommand)

// runsMiniInterface reports whether an opencode2 process's argv names the mini
// subcommand. The caller passes the argv of the process dispatch already
// identified as opencode2; the terminal title takes no part in it, because
// "opencode2 mini" is what the shell wrote for the command it was running and
// changes with the shell, the prompt, or a renamed pane.
//
// The subcommand is the first argument that is not a flag, rather than argv[1].
// opencode2 0.0.0-next-17444 documents global flags including --log-level and
// --wizard, and whether they may precede a subcommand was never established, so
// a fixed index would be a guess. Scanning the whole of argv for the word would
// be the other guess and the worse one: it claims a mini interface for `run
// mini`, where mini is the prompt. This rule errs towards missing a mini pane,
// which leaves its row exactly as it is today, rather than towards attaching a
// wrong reason to a blank one.
//
// A flag's own value can still absorb the subcommand position — `--log-level
// debug mini` reads debug as the subcommand — and that is the deliberate cost of
// not encoding a flag grammar this plugin cannot verify.
func runsMiniInterface(argv []string) bool {
	for _, argument := range argv[min(1, len(argv)):] {
		if strings.HasPrefix(argument, "-") {
			continue
		}
		return argument == miniSubcommand
	}
	return false
}

// attributeMiniInterface names the mini interface as the reason a pane reports
// no opencode title, when the pane's own opencode2 process says that is what it
// is running.
//
// Only an unavailable title is reattributed, which is what makes this
// attribution rather than parsing. A bare "OpenCode" title, a plugin route, a
// well-formed title matching no session and a store failure each already carry
// their own reason and keep it. The mini answer wraps the one it replaces, so
// the row blanks exactly as before and States needs no new entry.
func attributeMiniInterface(err error, processes []provider.Process) error {
	if !errors.Is(err, opencodeshared.ErrTitleUnavailable) {
		return err
	}
	for _, process := range processes {
		if process.Kind() == provider.OpenCode2 && runsMiniInterface(process.Argv) {
			return ErrMiniInterface
		}
	}
	return err
}
