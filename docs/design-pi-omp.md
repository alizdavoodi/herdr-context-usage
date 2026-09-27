# Spec: pi and omp providers

Extends [docs/design.md](design.md), whose scope is the four providers shipped
before this. Vocabulary is [CONTEXT.md](../CONTEXT.md); the decisions this spec
depends on are [ADR-0001](adr/0001-absolute-tokens-not-percentage.md),
[ADR-0004](adr/0004-provider-specific-session-correlation.md) and
[ADR-0006](adr/0006-two-providers-for-pi-and-omp.md).

## Problem Statement

A user running the `pi` or `omp` agent kind in a herdr pane sees an empty `ctx`
row. herdr already identifies both — they are distinct agent kinds in its closed
set, and it ships a lifecycle integration for each — so the pane is not the
problem. This plugin has no provider for either agent kind, and by the coverage
contract an agent kind without a provider is deliberately blank rather than
stale. The user therefore gets no context occupancy for two agents they use
daily, while adjacent claude and codex rows fill in normally, which reads as a
bug rather than as an unimplemented provider.

## Solution

Two providers, `pi` and `omp`, both reaching `Implemented` in the coverage
table: a measured context occupancy for the main thread of each pane, a
resolution mode recorded with every measurement, a cleared row at a compaction
boundary, and a blank row rather than a wrong number whenever the agent session
cannot be resolved.

Both read a JSONL transcript per agent session. Both report through the existing
`ctx` and `ctx_raw` metadata tokens; neither introduces a new one.

## User Stories

1. As a herdr user with a `pi` pane, I want its context occupancy in the
   sidebar, so that I can see how much room the agent session has left before it
   compacts.
2. As a herdr user with an `omp` pane, I want the same, so that I do not have to
   read two different agents two different ways.
3. As a herdr user running pi and omp side by side, I want each pane measured
   against its own agent, so that one agent's occupancy is never attributed to
   the other's pane.
4. As a herdr user, I want the row to go blank when the plugin cannot identify
   the agent session, so that I never act on a number belonging to a superseded
   agent session.
5. As a herdr user, I want the row cleared immediately after a compaction, so
   that a pre-compaction occupancy is never displayed as current.
6. As a herdr user, I want the row to keep its last value through a transient
   read failure, so that a momentary permission or I/O error does not flicker a
   healthy row to blank.
7. As a herdr user who has not installed the herdr integration for pi or omp, I
   want the documentation to tell me that is why my row is blank, so that I stop
   looking for a bug in the plugin.
8. As a herdr user, I want a compact display value such as `41k`, so that the
   sidebar stays readable at narrow pane widths.
9. As a coding agent, I want `ctx_raw` to be the exact integer for a pi or omp
   pane, so that I can compare occupancy against a threshold without parsing a
   rounded display value.
10. As a coding agent, I want an absent `ctx_raw` to mean unknown rather than
    zero, so that I do not conclude a full agent session is empty.
11. As a coding agent, I want a sub-agent session excluded from the reported
    occupancy, so that the number describes the main thread the pane is showing.
12. As a herdr user, I want a headless pi or omp run to be ignored, so that a
    scripted invocation with no pane to display never overwrites a real
    measurement.
13. As a herdr user who switches agent session inside a pi pane, I want the
    documented behaviour of the interval before the next turn boundary, so that
    I know a briefly wrong attribution is a known limit rather than a defect.
14. As a maintainer, I want each measurement to record which resolution mode
    produced it, so that a wrong attribution is diagnosable rather than
    mysterious.
15. As a maintainer, I want each measurement to record the transcript version it
    was read from, so that a format change in a floating dependency is visible
    in the output rather than only as a blank row.
16. As a maintainer, I want the two providers to remain separate packages, so
    that an omp release changing its transcript cannot silently corrupt pi
    readings.
17. As a maintainer, I want the arithmetic shared between them only where it was
    measured identical, so that shared code carries evidence rather than
    assumption.
18. As a maintainer, I want the coverage table to be the single statement of
    provider status, so that adding these two providers does not create a second
    place to check.
19. As a maintainer, I want a registry registration to be the only step that
    makes a provider live, so that adding a provider cannot half-succeed.
20. As a maintainer adding a seventh provider later, I want the existing
    property tests to cover it automatically, so that correctness does not
    depend on remembering to extend a list.
21. As a maintainer, I want the excluded token fields documented with the number
    that disqualified each, so that a future reader does not reintroduce one
    because its name looks right.
22. As a maintainer debugging a blank pi row, I want the measurement record to
    name the reason, so that I can tell an unresolvable agent session from an
    unreadable transcript.
23. As a maintainer, I want the pi and omp home directories overridable, so that
    tests can point at a fixture directory without touching the real store.
24. As a user who has exported the shared pi home variable, I want both agents
    read from where that variable points, so that the plugin looks where the
    agents actually wrote.

## Implementation Decisions

### Agent kinds and dispatch

The agent kinds are `pi` and `omp`, spelled exactly as herdr labels them.
herdr's set of agent kinds is a closed enumeration compiled into its binary, and
a user cannot extend it; `oh-my-pi` is not a value and neither is any
transposition of `omp`. A misspelled kind constant matches no agent label and
produces a permanently blank row with no error anywhere, so the spelling is
load-bearing.

Unlike the opencode pair, these two kinds are reported separately by herdr, so
dispatch needs no foreground-process inspection. Each provider is one
registration in the provider registry, which is the sole authority on which
agent kinds can be measured.

### Store

Each agent session is one JSONL transcript named for its start timestamp and its
session id, filed under a directory derived from the working directory the agent
started in, beneath the agent home. The homes default to `~/.pi/agent` and
`~/.omp/agent`, and both agents honour the same home override variable, so a set
override applies to both. The two agents derive their directory names
differently: pi wraps the whole absolute path, omp strips the home prefix. Each
provider therefore owns its own derivation.

Configuration carries the one override and hands it to each provider
unresolved, so an unset override leaves each on its own default home. Resolving
it once, to either agent's default, would point a pi pane at omp's store on any
machine running both.

### Context occupancy

Context occupancy is the sum of the newest completed assistant turn's input,
cache-read and cache-write token counts. This was verified on both agents
against turns where the cache counts were non-zero, and on pi the agent's own
per-turn total equals that sum plus the turn's output on every row, which proves
the cache count is an addend rather than a subset.

Four fields must never contribute:

- The per-turn **total** is the occupancy plus that turn's output. Reading it
  overstates every measurement. This is the inverse of codex, where the
  equivalent field is the one to prefer, so the two agents want opposite
  treatment of identically named data.
- The compaction record's **tokens-before** figure is that same per-turn total,
  and describes the agent session before the compaction. Wrong twice over.
- omp's **non-message** count is an independently computed overhead estimate. It
  is recomputed per turn, and was measured both above and below the prompt size
  in different agent sessions, so no subset relation holds in either direction.
- pi's **reasoning** count is a subset of the turn's output.

omp additionally records a prompt-size figure directly. It matched the sum
exactly on every observed turn, including cached ones, so it is used as an
oracle rather than as the source: the providers assert agreement and record a
diagnostic on divergence. Keeping the sum as the source means one formula backed
by measurement on both agents, with omp's extra field as a free cross-check.

### Compaction and just-compacted

Both agents write a dedicated compaction record. The transcript walk runs
backwards from the end and **must stop at that record**, reporting
just-compacted, which clears the metadata tokens.

Two distinct hazards make this non-optional, and they fail in opposite
directions:

- On omp, walking past the compaction record reaches the last pre-compaction
  turn. Measured, that is a 3.2x overstatement — a nearly full window reported
  for an agent session that is a third full.
- On pi, the compaction record **carries its own usage**, being the cost of
  generating the summary. A walk that stops at "the newest record containing a
  usage" therefore reads the compaction record itself and reports roughly a
  thousand tokens. That is plausible for a freshly compacted agent session and
  so would never be questioned.

Detection keys on the record type, never on the presence of a usage field.

After the next turn boundary both agents report the post-compaction occupancy
through the ordinary fields, with no zeroing of the breakdown. No compensating
logic is needed once the walk stops correctly.

### Resolving which agent session a pane is on

Resolution uses herdr's reported agent session and nothing else. herdr never
derives it; the agent's own integration pushes it, preferring an absolute
transcript path and falling back to a session id. A path is used directly. An id
is resolved by matching the transcript whose name ends with that id, under the
provider's home, and a match that is not unique is treated as unresolved.

There is no working-directory fallback, for two reasons that bear on both
agents. Both file an agent session under the directory the agent *started* in,
so a pane that has since moved resolves to nothing or to an unrelated agent
session — measured only on pi, on this machine, where the directory for this
repository exists but is empty while the real agent session sits elsewhere. And
neither transcript marks a sub-agent session, so a directory-scoped match has
nothing to exclude one with.

A third reason was recorded here and is withdrawn: that omp has no
screen-detection manifest, so an omp pane without its integration is never
labelled an agent by herdr and never reaches this plugin. Observed on herdr
0.8.0 with omp 18.1.4, herdr labels such a pane from the process name and it
does reach this plugin, which resolves it to nothing like any other pane with
no reported agent session.
[ADR-0004](adr/0004-provider-specific-session-correlation.md) records the
withdrawal and why the decision stands without it.

Installing the herdr integration is therefore a documented precondition for
both agent kinds, recorded in the coverage table's correlation column the way
the existing providers already name herdr's reported agent session first. What
it supplies is the reported agent session, not the pane's standing as an agent.

### Main thread

Both integrations gate on the agent's root session and on interactive mode, so a
sub-agent session never reports an agent session and a headless run never
reports at all. The main-thread invariant is enforced upstream of this plugin,
which is why no transcript-level sub-agent filter is required.

### Schema drift

Detection is by field presence. Both agents stamp the same schema version on the
session record while carrying different field sets, so version distinguishes
neither the two agents nor a breaking change within one. Gating on it would
reject a harmless increment and accept a breaking change at the same number. The
observed version is instead carried into the measurement record, so a format
change in a floating dependency appears in the output rather than only as a
blank row.

### Known limit: agent session switching in pi

pi's integration reports its agent session on session start and at each turn
boundary, but not on a session switch; omp's integration handles the switch. A
pi pane whose agent session is switched therefore keeps the previous agent
session reported until the next turn boundary, and the plugin will measure the
previous transcript in that interval. This is documented rather than defended:
the common switch resumes another agent session in the same directory, which no
directory comparison can detect, and the interval closes at the same turn
boundary all measurement is anchored to. The gap is worth reporting upstream,
since omp's integration demonstrates the event is available.

## Testing Decisions

A good test here asserts the external contract: the metadata token values
written for a pane, or the decision to clear them, or to leave them alone. It
does not assert how a transcript was walked, which functions were called, or in
what order. The contract is what a herdr user sees in the sidebar and what an
agent reads back, and nothing below that is worth pinning.

**Primary seam: the top-level refresh, driven through the fake herdr client.**
This is the highest seam available and the only one where the contract is
observable. Fixtures are real transcripts written to a temporary directory, with
configuration pointing the provider's home at it and the fake client returning
an agent whose kind and reported agent session are set per case. The prior art
is the existing opencode2 app-level tests, which cover that whole provider this
way including its compaction boundary; each new provider gets an equivalent
file.

Cases to cover at this seam, per provider:

- a measured occupancy reported as both a display value and an exact integer
- an agent session reported as a path, used directly
- an agent session reported as an id, matched to its transcript
- an id matching no transcript, and an id matching more than one: both blank
- no agent session reported at all: blank
- a compaction record as the newest record: tokens cleared, just-compacted
  recorded
- a turn boundary after a compaction: the post-compaction occupancy reported
- a transient read failure: the previous value preserved, not cleared
- a transcript with no completed assistant turn: blank rather than zero
- the resolution mode present in every measurement record
- the observed schema version present in every measurement record

Two cases exist only for one agent each: pi's compaction record carrying its own
usage must not be read as a turn, and omp's prompt-size oracle disagreeing with
the sum must produce a diagnostic while still reporting the sum.

**Secondary seam: the transcript reader within each provider package.** Used
only where the input space is wide and app-level setup adds nothing — a
malformed line, an absent file, a truncated final line, a turn carrying no usage
counts, cache counts present and absent. The prior art is the existing claude
reader tests.

**The two new providers add no seam of their own.** Substitutability comes
from the provider registry: every provider is a value of the
`provider.Provider` interface the provider root declares, and the registry
builds one of each from the plugin's configuration. The registry-derived
property tests therefore cover both new providers automatically: that each
declares a superseded answer. Nothing tests for an answer declared both
superseded and merely behind, or for two providers claiming one kind: there is
one list of superseded answers, and the compiler rejects a duplicate kind in
the registry.

## Out of Scope

Window limit, headroom and any percentage or gauge. omp reports its own window
limit and this was considered and declined; see the superseding condition
recorded in ADR-0001. That figure may be used as a verification oracle while
checking arithmetic, but it is not read at runtime and not displayed.

Session spend, plan quota and dollar cost, which the transcripts also carry.
These are different measurements and remain outside this plugin's one job.

omp's SQLite databases, its per-process log files, and its terminal-sessions
directory. The transcript is the source; the log was read once as evidence while
establishing the arithmetic and is not an interface.

Headless modes of either agent. They report no agent session by design.

Installing the herdr integration on the user's behalf, or writing the user's
herdr configuration. Both remain the user's actions, consistent with the
plugin's existing refusal to edit the sidebar configuration.

A plugin command line; see ADR-0005.

Any change to the four existing providers, except the documentation fix noted
below.

## Further Notes

**One precondition is unmet.** pi's recovery after a compaction is not yet
measured: its compaction record is the newest record in the only compacted pi
agent session available, so the just-compacted detector is proven but the
post-compaction reading is not. omp's was measured twice. This is a stated
precondition on the pi provider's ticket, not an assumption to carry silently —
one further turn in that agent session closes it. The risk is low, because pi's
per-turn total identity held across every turn measured, but it is the last
inferred link in an otherwise measured chain.

**An unrelated documentation fix belongs with this work.** The minimal
interactive interface of opencode2 sets no terminal title — a pane running it
reports the raw shell command instead — so such a pane can never satisfy the
title correlation the opencode2 provider depends on and stays blank permanently.
This is a third invisible cause for the README's existing list of reasons an
opencode row never fills in, alongside the environment variable and the palette
toggle. It was found by accident while running an agent in a pane during this
work.

**Versions these measurements were taken against**: pi 0.84.2, omp 17.3.5,
herdr 0.8.0, and omp 18.1.4 for the withdrawal recorded under "Resolving which
agent session a pane is on". Neither agent is pinned — both are run from a
flake reference that resolves to whatever is current — so the transcript
formats float. This is the reason the observed schema version is carried into
the measurement record, and the reason the two providers stay separate.

**Documentation to update when the providers ship**: the coverage table gains a
row per agent kind naming the store, the arithmetic and the correlation; the
provider and correlation sections of design.md gain matching entries; and
ADR-0004 gains the pi and omp correlation entry together with the integration
precondition. None of that should land before the code, because the coverage
table is the source of truth for provider status and would otherwise claim
support that does not exist.
