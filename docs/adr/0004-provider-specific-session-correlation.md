# Correlate panes to sessions per-provider, and never by recency

A pane must be matched to the agent session it is displaying before its
occupancy can be read. The obvious fallback, when no session id is available, is
"newest session in this directory". We ban it. On the only live `opencode2`
sample available, recency, the TUI's own tab file, and terminal-title matching
each produced a *different* answer, and the pane was on the third-most-recent
session in its directory. Recency is not merely fragile here; it was measurably
wrong on the first case tested.

Resolution is therefore per-provider: herdr's `agent_session` when an official
integration has reported one, otherwise whatever is actually correct for that
agent — `foreground_cwd` derivation for `claude` and `codex`, `terminal_title`
matching against `OC | <session title>` for `opencode2` — and blank when nothing
resolves.

Claude's `foreground_cwd` fallback has two diagnostic modes. An exact PID,
working-directory and agent-kind match in Claude's process registry is recorded
as `process_registry`; the structural fallback that accepts the sole transcript
under the encoded working directory remains `foreground_cwd`.

## Consequences

Every measurement records its resolution mode, so a wrong attribution is
diagnosable rather than mysterious.

Roughly 10% of `opencode2` sessions are untitled and render a bare `OpenCode`
title. Those panes show nothing. A blank row is the correct output here: with
14 sessions sharing a single repository directory, a guess is far more likely to
be wrong than right, and a confident wrong number destroys trust in every other
row.

The `opencode2` path depends on herdr surfacing the TUI's OSC title in
`terminal_title`. Confirmed against a live pane: herdr reported
`OC | Probe test response`, which resolved to exactly the expected session id
when matched on directory plus title prefix.

Title correlation has a known residual: plugin routes also emit `OC | ` titles
but do not identify an agent session. In v1 that text is a plugin id; in
opencode2 it is a plugin name. Plugin titles are not truncated, so they can only
reach exact title matching; a title over 40 characters without a trailing
ellipsis therefore cannot be a session title. That is only a partial
discriminator: shorter plugin titles remain indistinguishable from an exact
root-session title collision. No root-session title in this installation
collides with a present plugin id. Thus blank on ambiguity is not an absolute
attribution guarantee for title-based correlation.
The only complete fixes are a discriminator herdr does not expose or a native
`agent_session`, which retires the fallback. The fallback remains worthwhile:
without it these providers cannot report sessions when no integration supplies
an agent session.

Recency remains tempting because it is how `opencode2 --continue` picks a
session. That answers "what would resume here", not "what is this pane showing".
