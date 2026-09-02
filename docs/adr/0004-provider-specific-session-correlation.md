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

`pi` and `omp`, the two agent kinds specified in
[design-pi-omp.md](../design-pi-omp.md), sit at the other end of that range:
they resolve from herdr's reported agent session **and nothing else**, with no
fallback at all. A path is used directly; an id is matched against the
transcript whose name ends with it under that provider's own home; a match that
is not unique is treated as unresolved rather than chosen between.

Three independent reasons put the fallback out of reach, two of them bearing on
each agent. pi files an agent session under the directory it *started* in, so a
pane that has since moved resolves to nothing or to an unrelated agent session —
measured on this machine, where the directory for this repository exists but is
empty while the real agent session sits elsewhere. Neither agent's transcript
marks a sub-agent session, so a directory-scoped match would have nothing to
exclude one with, and the main-thread guarantee this plugin makes would become a
guess. And omp ships no screen-detection manifest at all, so without its
integration installed an omp pane is not labelled an agent by herdr and never
reaches this plugin to be resolved in the first place.

Installing the herdr integration is therefore a **precondition** for both agent
kinds, not an optional improvement on a fallback that works without it. The
difference from the four providers above is that none of those needs anything
installed in order to correlate at all: each works from what herdr already
reports about a pane, and goes blank where that signal is absent or has been
switched off. pi and omp correlate nothing until their integration is installed.
Recorded against pi 0.84.2, omp 17.3.5 and herdr 0.8.0; neither agent is pinned.

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

A user who has not installed the herdr integration meets the precondition
recorded above, and the two agent kinds hand it to them differently. herdr
already labels a pi pane a pi agent, so that pane has a row and the row is
permanently blank, with nothing anywhere to explain it — the same failure mode
the opencode title causes produce. An omp pane is not labelled an agent at all
until its integration is installed, so there is no row to be blank: the pane is
absent from the agent list entirely. pi's symptom is a row that never fills in
and omp's is an agent that never appears; both are silent about the cause, and
each sends a user looking somewhere different, which is why the precondition is
written down for users and for agent consumers rather than left here. Where
there is a blank row it remains the correct output, because with no reported
agent session there is no signal left to be right about; it is simply
indistinguishable from an agent that has not yet taken a turn, so the
documentation has to carry a distinction the row cannot.

Recency remains tempting because it is how `opencode2 --continue` picks a
session. That answers "what would resume here", not "what is this pane showing".
