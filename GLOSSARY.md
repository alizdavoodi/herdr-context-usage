# Herdr Context Usage

A herdr plugin that reports how full each coding agent's context window is, into
herdr's agent sidebar and to any agent that asks. This document fixes the
vocabulary; it deliberately contains no implementation detail.

## Language

### What we measure

**Context occupancy**:
The number of tokens resident in a model's context window for the agent session
currently owning a pane. Rises through a turn, falls at compaction, resets when
a new session starts.
_Avoid_: context usage, token usage, context size, context length

**Window limit**:
The maximum context occupancy a model will accept before refusing or compacting.
Known for some agent kinds, unknowable for others.
_Avoid_: context window, max tokens, capacity

**Headroom**:
Window limit minus context occupancy.
_Avoid_: remaining, tokens left

### What we deliberately do not measure

These are named so they stay distinguishable from context occupancy, not because
the plugin reports them.

**Session spend**:
Cumulative tokens billed across a whole session, including every superseded
turn. Monotonic, unaffected by compaction. A cost measure, not a pressure
measure.
_Avoid_: total tokens, usage

**Plan quota**:
How much of a subscription's rolling allowance has been consumed, expressed
against a time window rather than a context window.
_Avoid_: rate limit, usage limit

### Agents and sessions

**Agent kind**:
herdr's canonical label for the program occupying a pane, drawn from a closed
set. Coarser than reality: `opencode2` is folded into `opencode`.
_Avoid_: agent type, agent name

**Provider**:
The part of this plugin that knows how to extract context occupancy for one
real-world agent. Not one-to-one with agent kind, because one kind can front
two incompatible programs.
_Avoid_: adapter, extractor, backend

**Agent session**:
One continuous conversation with its own context window, identified by the
agent's own session id. Survives the agent restarting against it; a pane may
host several over its life, one at a time.
_Avoid_: conversation, chat, thread

**Main thread**:
The agent session a pane is displaying, excluding any sub-agent sessions it
spawns. The only thread whose occupancy this plugin reports.
_Avoid_: parent session, primary session

**Resolution mode**:
Which method identified the agent session for a pane, recorded alongside every
measurement so a wrong attribution is diagnosable rather than mysterious.
_Avoid_: lookup strategy, match type

### Change and reporting

**Turn boundary**:
The moment an agent finishes responding and returns control. Every reading is
current at a turn boundary, which is why measurement is anchored to them.
_Avoid_: completion, stop event

**Call boundary**:
The moment one model API call inside a turn completes. Context occupancy is
stable between call boundaries, not only between turn boundaries. A value read
mid-turn is the occupancy of the last call a refresh observed, a lower bound
on the call in flight within the same session and absent a compaction since.
A turn boundary is also a call boundary.
_Avoid_: step, tool boundary

**Compaction**:
An agent replacing its accumulated transcript with a summary, causing context
occupancy to fall sharply within one session.
_Avoid_: compact, summarization, truncation

**Just-compacted**:
The interval after a compaction and before the next turn, during which the true
occupancy is not yet expressed anywhere readable. Distinct from unknown, and
distinct from zero.
_Avoid_: pending, stale

**Metadata token**:
A named string a plugin attaches to a herdr pane, which herdr renders in the
sidebar and returns from its own read commands. The plugin's only channel to
both the display and the query surface.
_Avoid_: tag, label, field, variable
