# Agent hooks may trigger a refresh mid-turn, as an opt-in the user installs

[ADR-0002](0002-host-triggered-reads-not-agent-hooks.md) anchored every read to
a host event on the premise that occupancy changes only at turn boundaries. It
does not. Every supported store already records occupancy once per API call:
an agentic turn is a run of calls, each one's prompt larger than the last, and
the readers return the right figure whenever they run. What they lacked was a
trigger between turn boundaries. A long turn left the row at the previous turn's
size until something else in Herdr happened to fire, which is exactly when an
operator is deciding whether a pane is about to compact.

Every agent has a hook that fires per tool call, and some per API call, but no
two share a format. So host events remain the architecture, and an agent hook
is an accelerator layered on top of it. The plugin declares one Herdr action,
`refresh`, which runs the same full refresh an event does. The README carries a
snippet per agent that invokes it. The user installs that snippet by hand, or
does not, and every row stays correct either way; only its cadence changes.

## What this amends

ADR-0002 rejected agent-side hooks because of the installer: three foreign
config formats written, three uninstall paths, breakage whenever one changed.
That objection stands, so the plugin still ships no installer and writes into
no agent's configuration. What changes is that a hook is now documented, not
forbidden. A snippet the user pastes is owned by the user, and a format change
breaks one accelerator rather than the plugin.

[ADR-0005](0005-no-plugin-cli.md) rested on the same premise when it called
the staleness window normally empty, and is corrected on that point only. Its
decision is untouched. The action is a trigger, not a query: `herdr plugin action invoke` returns the started command's log record,
not its output, which rules it out as a query interface but suits a trigger
that only needs to start the run. Nothing goes on `$PATH`, and a hook needs no
plugin root, because Herdr resolves the action by plugin id.

## Considered options

**A resident store watcher.** One uniform trigger for every agent, watching the
JSONL directories and SQLite stores. Rejected for the reason ADR-0002 rejected
polling: Herdr supervises no daemon, and a `[[startup]]` hook that forked one
would be rerun on every live handoff with nothing restarting it when it died.

**Claude's `statusLine`.** Exact numbers at a 300 ms debounce, for one agent
only, and it takes the single statusLine slot. Rejected on ADR-0002's grounds.

**Pushing the value into the model's context** through `additionalContext` or
a rewritten tool result. Unneeded: any agent already reads the latest value
from Herdr whenever it asks, mid-turn included, and pushing costs tokens on
every tool call for the rest of the session.

**Herdr's `pane.scroll_changed` or `pane.output_matched`.** Neither is a call
boundary, and the first fires per output line with no debounce.

**An action that refreshes only the invoking pane.** Cheaper per call, but the
action context Herdr passes names the *focused* pane, not the pane whose hook
ran the CLI, so it cannot identify the caller. The action does the full
refresh instead, at roughly 50 ms per run; a hook that wants a single pane
would have to pass its own `HERDR_PANE_ID` some other way.

## Consequences

**The meaning of a mid-turn value changes.** Occupancy is stable between call
boundaries, not turn boundaries. A value read mid-turn is the occupancy of
the last call a refresh observed, and within one session with no compaction
since it is a lower bound on the call in flight, which already includes that
call's output and any tool results since. The bound does not survive a
compaction or a session change the row has not yet observed, and a refresh can
fail transiently and leave the old value standing, so the value is the last
successful reading, not a guarantee. For codex the figure is `total_tokens`,
which includes the call's own output, as the provider table says. At a turn
boundary the reading and the call coincide, as before.

**codex had to read a different row for the hook to be worth it.** Its
`token_count` row lands after a step's tools drain, so a hook firing on tool
completion would read the previous call. Recent rollouts also carry a
`token_usage_record` per response, written before the tool runs, and the reader
now takes whichever of the two is later in the file. For one response they carry the
same total, checked across every rollout on this machine that has both.

**Overlapping runs became routine, so reports are now sequenced.** Herdr runs
plugin commands concurrently and does not serialize them. Before hooks, 7 of 43
logged runs already overlapped, and a slower, older run could overwrite a
newer value. Every report and clear now passes `--seq`, the wall-clock nanoseconds at
which its refresh round listed the agents, and Herdr drops one whose sequence
is not above the last accepted one, for a clear as for a report. A write without `--seq` bypasses that
ordering and leaves the high-water mark in place, so nothing in the plugin may
write unsequenced. A clock stepped backwards freezes the row: every report
and clear for this source is dropped until wall time passes the old high-water
mark, which is as long as the step. A sequence that survives that would need an
allocator shared across plugin processes, not a process-local counter. Every
write in a round is a verdict on that round's agent snapshot, so the snapshot's
time orders all of them: a clear from an older snapshot cannot outrank a
measurement from a newer one, and a measurement from an older snapshot cannot
outrank a newer snapshot's clear. The price is that two rounds measuring the
same live pane rank by snapshot, not by read, so the slower reader's fresher
value can lose to the faster one's; the row then lags, by as many calls as
pass before the next successful refresh, and is still a lower bound.

**Bursts are tolerated.** Ten concurrent invocations each ran to completion; no
`plugin_command_limit_reached` appeared. A Claude turn that fires three
`PostToolUse` hooks within a second is well inside that.

**Adding the action needed no reload.** Herdr re-reads a locally linked
plugin's manifest on demand, so `refresh` was listed and invocable as soon as
the manifest changed.

Measured against Herdr 0.9.3 on macOS. The concurrency and sequencing results
were not repeated on Linux.
