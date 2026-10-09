# Read transcripts on herdr events rather than installing agent-side hooks

Amended by [ADR-0008](0008-opt-in-agent-hooks-refresh-mid-turn.md). This
decision took a turn boundary to be the only moment occupancy changes. It
changes at every API call, so a hook the user installs may now trigger a
refresh between turns. Host events remain the architecture, and the plugin
still writes into no agent's configuration.

The obvious way to learn that a turn ended is to have the agent tell you: a
Claude Code `Stop` hook, codex hooks in `~/.codex/config.toml`, an opencode
plugin. A sibling project does exactly this, because tmux cannot signal a turn
boundary and it has no other option. herdr can: `pane.agent_status_changed`
fires at the same moment, and the plugin already receives `HERDR_PANE_ID` and
the event payload. So we read each agent's own store when herdr tells us to, and
install nothing into any agent's configuration.

## Considered options

**Agent-side hooks.** Push-quality cadence, but a three-way installer writing
into three foreign config formats, three uninstall paths, and breakage whenever
any of those formats changes.

**Claude `statusLine`.** Used by another plugin for its rate-limit row. Rejected
on principle rather than measurement: `statusLine` is singular, so adopting it
means displacing whatever the user already runs there, and its cadence is
per-render rather than per-turn.

**Polling on a timer.** No daemon exists to run it — herdr's `[[startup]]` is
documented as one-shot, not a supervised daemon.

## Consequences

Correlating a pane to a session becomes our problem rather than the hook's,
since a hook would have been handed the transcript path. This is the main cost
and is handled in ADR-0004.

An agent that keeps no readable on-disk store cannot be supported this way. All
four current providers have one; a future fifth might not, at which point
agent-side hooks return as a per-provider fallback rather than the architecture.
