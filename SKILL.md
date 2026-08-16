---
name: herdr-context-occupancy
description: Read context occupancy reported by the herdr-context-usage plugin. Use when an agent needs to know how full its own or another Herdr pane's model context is.
---

# Discover context occupancy

This plugin publishes each supported agent's context occupancy as Herdr
metadata tokens. The plugin has no query command; read the values from Herdr.

## Read the tokens

For the current pane:

```bash
herdr pane current --current | jq '.result.pane.tokens // {}'
```

For every live agent, including other panes:

```bash
herdr agent list | jq '.result.agents[] | {pane_id, agent, agent_status, tokens: (.tokens // {})}'
```

Neither command takes `--json`; both emit JSON unconditionally. Match a
neighbour by its `pane_id` in the agent-list result. Herdr omits the `tokens`
key when a pane has no tokens rather than returning an empty object; some JSON
responses serialize that missing map as `null`. Use `.tokens // {}` before
reading it.

The two token names are:

- `ctx` is the compact display value, with forms such as `840`, `92k`, and
  `1.2M`.
- `ctx_raw` is the exact integer value for machines.

Read `ctx_raw` for decisions and calculations. Never parse `ctx`; its display
shape is deliberately compact and varies with the magnitude.

## Interpret the value

The number is context occupancy: the number of tokens resident in the model's
context window for the agent session currently owning the pane. It is not
session spend, which is the cumulative number of tokens billed across the
whole session, including superseded turns. It is not plan quota, which is a
subscription's rolling allowance over a time window. Occupancy is a pressure
measure; session spend is a cost measure; plan quota is neither a context-window
measurement nor a substitute for one.

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
