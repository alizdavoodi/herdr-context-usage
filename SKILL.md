---
name: herdr-context-occupancy
description: Read context occupancy reported by the herdr-context-usage plugin. Use when an agent needs to know how full its own or another Herdr pane's model context is.
---

# Read context occupancy

Run `herdr agent list` and inspect `.result.agents[].tokens`.

- Read `ctx` for the compact human display, such as `92k`.
- Read `ctx_raw` for the exact integer context occupancy.
- Treat an absent `tokens` object or absent `ctx_raw` as unknown, never as zero.
- Match panes by `pane_id` or the agent fields returned by Herdr; do not invoke the plugin binary directly.

For the current pane, run `herdr pane current --current` and inspect `.result.pane.tokens`.
