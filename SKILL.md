---
name: herdr-context-occupancy
description: Read context occupancy reported by the herdr-context-usage plugin. Use when an agent needs to know how full its own or another Herdr pane's model context is.
---

# Discover context occupancy

This plugin publishes each supported agent's context occupancy as Herdr
metadata tokens. The plugin has no query command; read the values from Herdr.

## Read the tokens

Before running the queries below, fail closed if this is not a Herdr pane (or
the environment did not inherit the pane id):

```bash
if [ -z "${HERDR_PANE_ID:-}" ]; then
  echo 'HERDR_PANE_ID is unset; run these queries from a Herdr pane.' >&2
  exit 1
fi
```

Your pane id is in `$HERDR_PANE_ID`. Confirm that id and retain the identifying
fields while reading your current pane:

```bash
echo "$HERDR_PANE_ID"
herdr pane current --current | jq '.result.pane | {pane_id, agent, foreground_cwd, tokens: ((.tokens // {}) | if has("ctx_raw") then .ctx_raw |= tonumber else . end)}'
```

To list the other live panes, keep their identifying fields and exclude your
own `pane_id`:

```bash
herdr agent list | jq --arg me "$HERDR_PANE_ID" '.result.agents[] | select(.pane_id != $me) | {pane_id, name, agent, foreground_cwd, tokens: ((.tokens // {}) | if has("ctx_raw") then try (.ctx_raw |= tonumber) catch . else . end)}'
```

Neither command takes `--json`; both emit JSON unconditionally. Match a
neighbour by its human-readable `name` when present, then use that row's
`pane_id` as its unambiguous identity. Never match a neighbour by token value.
The current-pane response also includes `pane_id`, so it gives you the identity
to compare with `$HERDR_PANE_ID`. A row can have no `name`; `pane_id` is still
the reliable identifier.

On the wire, Herdr omits the `tokens` key when a pane has no tokens; it does not
send `{}`. `.tokens // {}` is a jq-only fallback that substitutes an empty
object for the absent key so the filter can safely inspect it. Herdr token
values, when present, are JSON strings, including `ctx_raw`; the recipes above
convert only valid `ctx_raw` strings to JSON numbers with `tonumber`. The jq
`try` keeps a malformed value from truncating the rest of the agent list; the
plugin itself writes valid integer strings.

The two token names are:

- `ctx` is the compact display value, with forms such as `840`, `92k`, and
  `1.2M`.
- `ctx_raw` is a string containing the exact integer value for machines.

Read `ctx_raw` for decisions and calculations, converting it with `tonumber`
as shown above. Never parse `ctx`; its display shape is deliberately compact
and varies with the magnitude.

## Interpret the value

The number is context occupancy: the number of tokens resident in the model's
context window for the agent session currently owning the pane. It is not
session spend, which is the cumulative number of tokens billed across the
whole session, including superseded turns. It is not plan quota, which is a
subscription's rolling allowance over a time window. Occupancy is a pressure
measure; session spend is a cost measure; plan quota is neither a context-window
measurement nor a substitute for one.

The reported occupancy is for the main thread only. It excludes agent sessions
spawned as sub-agents by the pane's main thread.

This plugin reports no percentage. It does not know a universal window limit,
so it cannot tell you a model's headroom or how close the occupancy is to that
limit. Use the absolute `ctx_raw` count instead of inventing a percentage.

An `opencode` pane can be blank permanently rather than temporarily. Both
opencode versions read the session Herdr's opencode integration reports, when
it reports one. Otherwise a pane is correlated through the terminal title its
TUI sets, and that title is its only correlation signal: opencode v1 stops
setting it when `OPENCODE_DISABLE_TERMINAL_TITLE` is `true` or `1`, either
version stops when the TUI's own `Disable terminal title` command has been
used, and `opencode2 mini` never sets it at all. A pane in that state reports no
occupancy at all, for any session, until the title comes back. Do not read a
blank `opencode` row as an idle or empty agent.

A `pi` pane can be blank permanently for a different reason: its Herdr
integration is not installed. `pi` is correlated to an agent session only
through the agent session that integration reports, and it has no fallback of
any kind, so a pane whose integration is missing offers nothing to resolve and
stays blank for as long as that is true. Nothing in the row distinguishes this
from a `pi` agent that has not yet taken a turn. Do not read a blank `pi` row as
an idle or empty agent either, and do not conclude from a run of blank `pi` rows
that the neighbours are quiet.

A `pi` row also carries one known limit that is bounded rather than permanent.
pi's integration reports its agent session at session start and at each turn
boundary, but not when the agent session is switched inside the pane, so between
such a switch and that pane's next turn boundary the row describes the agent
session it was showing before. The value is current rather than stale — it is a
real measurement of the wrong agent session — and the interval closes at the
next turn boundary, which is the same event every reading here is anchored to.
That session-switch behaviour was observed against pi 0.84.2 and Herdr 0.8.0,
and pi is not pinned to a version. The permanent blank above follows from pi
having no fallback at all rather than from any observation of its own.

An `omp` pane can be blank permanently for the same reason a `pi` one can: its
Herdr integration is not installed. `omp` is correlated only through the agent
session that integration reports and has no fallback either, so a pane whose
integration is missing offers nothing to resolve and stays blank for as long as
that is true. The pane is still listed — Herdr labels it an `omp` agent from
its own process, independently of the integration — so an `omp` row that is
present and blank tells you nothing about whether the integration is there. Do
not read a blank `omp` row as an idle or empty agent, and do not read one as
proof that the integration is working. Observed against omp 18.1.4 and Herdr
0.8.0; omp is not pinned to a version.

A `hermes` pane can be blank permanently for the same reason: its Herdr
integration is not installed, and `hermes` is correlated only through the
session id that integration reports, with no fallback. It is also blank while
the reported session has ended, which on Hermes's desktop, TUI and ACP
platforms can last indefinitely: those report a session only at start and at
`/new`, so a session switched inside the app is never seen. Do not read a blank
`hermes` row as an idle or empty agent. Observed against Hermes Agent 0.21.5 and
Herdr 0.9.1; Hermes is not pinned to a version.

Tokens are only as fresh as the last event that wrote them. Occupancy changes
at call boundaries, each completed model API call inside a turn, while Herdr's
own events fire at turn boundaries. Between turns the staleness window is
empty. Mid-turn, a pane whose user installed the optional refresh hook updates
at each tool or model call; one without it can lag until the turn ends. A
mid-turn value is the occupancy of the last refresh that succeeded. Within one
session, with no compaction since, it is a lower bound on the call in flight;
across a compaction or a session change it is not, until a refresh observes
that boundary. A blank or absent token means unknown, never zero.
The plugin clears tokens when a semantic state proves that the previous value
no longer describes the current session, such as no resolvable session or a
just-compacted session. A transient I/O failure can preserve the last value;
do not treat that preserved value as a new measurement.

Do not invoke the plugin binary directly. Publish-and-read through Herdr is the
entire query interface.
