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
opencode versions are correlated to a session through the terminal title their
TUI sets, and that title is their only correlation signal: opencode v1 stops
setting it when `OPENCODE_DISABLE_TERMINAL_TITLE` is `true` or `1`, and either
version stops when the TUI's own `Disable terminal title` command has been
used. A pane in that state reports no occupancy at all, for any session, until
the title comes back. Do not read a blank `opencode` row as an idle or empty
agent.

Tokens are only as fresh as the last event that wrote them. Occupancy changes
at turn boundaries, and Herdr events fire at turn boundaries, so the normal
staleness window is empty. A blank or absent token means unknown, never zero.
The plugin clears tokens when a semantic state proves that the previous value
no longer describes the current session, such as no resolvable session or a
just-compacted session. A transient I/O failure can preserve the last value;
do not treat that preserved value as a new measurement.

Do not invoke the plugin binary directly. Publish-and-read through Herdr is the
entire query interface.
