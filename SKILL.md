---
name: herdr-context-occupancy
description: Read context occupancy reported by the herdr-context-usage plugin. Use when an agent needs to know how full its own or another Herdr pane's model context is.
---

# Discover context occupancy

This plugin publishes each supported agent's context occupancy as Herdr
metadata tokens. The plugin has no query command; read the values from Herdr.

## Read the tokens

Your pane id is in `$HERDR_PANE_ID`. Confirm that id and retain the identifying
fields while reading your current pane:

```bash
echo "$HERDR_PANE_ID"
herdr pane current --current | jq '.result.pane | {pane_id, agent, agent_status, foreground_cwd, tokens: ((.tokens // {}) | if has("ctx_raw") then .ctx_raw |= tonumber else . end)}'
```

To list the other live panes, keep their identifying fields and exclude your
own `pane_id`:

```bash
herdr agent list | jq --arg me "$HERDR_PANE_ID" '.result.agents[] | select(.pane_id != $me) | {pane_id, agent, agent_status, foreground_cwd, tokens: ((.tokens // {}) | if has("ctx_raw") then .ctx_raw |= tonumber else . end)}'
```

Neither command takes `--json`; both emit JSON unconditionally. Match a
neighbour by choosing its `pane_id` from this list, never by matching its token
value to another row. The current-pane response also includes `pane_id`, so it
gives you the identity to compare with `$HERDR_PANE_ID`.

On the wire, Herdr omits the `tokens` key when a pane has no tokens; it does not
send `{}`. `.tokens // {}` is a jq-only fallback that substitutes an empty
object for the absent key so the filter can safely inspect it. Herdr token
values, when present, are JSON strings, including `ctx_raw`; the recipes above
convert only `ctx_raw` to a JSON number with `tonumber`.

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

Tokens are only as fresh as the last event that wrote them. Occupancy changes
at turn boundaries, and Herdr events fire at turn boundaries, so the normal
staleness window is empty. A blank or absent token means unknown, never zero.
The plugin clears tokens when a semantic state proves that the previous value
no longer describes the current session, such as no resolvable session or a
just-compacted session. A transient I/O failure can preserve the last value;
do not treat that preserved value as a new measurement.

Do not invoke the plugin binary directly. Publish-and-read through Herdr is the
entire query interface.
