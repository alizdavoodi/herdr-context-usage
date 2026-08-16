# Herdr Context Occupancy

Herdr Context Occupancy reports the **context occupancy** of the main thread in
each coding-agent pane whose provider is implemented. It writes two Herdr
metadata tokens:

- `ctx`: a compact display value such as `92k`.
- `ctx_raw`: the exact integer, such as `92481`, for machines and agents.

## Install

Requirements:

- Herdr `0.8.0` or newer.
- Linux or macOS.
- Go 1.22 or newer available to Herdr's plugin build environment.

This repository is not published as `alizdavoodi/herdr-context-usage` on GitHub
yet. For a local checkout, link it with:

```bash
herdr plugin link --enabled /path/to/herdr-context-usage
```

When a repository is published, replace `OWNER/REPO` with its actual path and
run `herdr plugin install -y OWNER/REPO`. Herdr also accepts an optional
`OWNER/REPO/SUBDIR` path for a plugin in a subdirectory.

The manifest's `[[build]]` command builds the plugin with `CGO_ENABLED=0`. If
that build fails during installation, installation stops with the build error;
fix the Go or source error and run it again. The same manifest supplies the
build command for the local plugin workflow. The binary is not put on `$PATH`:
Herdr invokes it from the plugin root with the relative command
`./herdr-context-usage`.

## Add the sidebar row

**Installation alone does not make a number appear.** Herdr renders a plugin
token only when the user names that token in their own sidebar configuration,
and this plugin deliberately does not edit `~/.config/herdr/config.toml`.
Add this to that file:

```toml
[ui.sidebar.agents]
rows = [
  ["state_icon", "workspace", "tab"],
  ["agent", "$ctx"],
]
```

If `[ui.sidebar.agents]` already exists, add `["agent", "$ctx"]` to its
existing `rows` list instead of adding a second table. Reload the configuration
after saving:

```bash
herdr server reload-config
```

Focus an agent whose provider is marked `Implemented` in the
[coverage table](#agent-kinds-and-provider-coverage), or let it complete a
turn. The plugin refreshes on
`pane.agent_status_changed`, `pane.focused`, `pane.exited`, and `pane.closed`,
and the `[[startup]]` hook restores tokens after a Herdr server restart.
Sidebar rows are not visible in Herdr's collapsed or mobile layouts.

## What the number means

Context occupancy is the number of tokens resident in the model's context
window for the agent session currently owning the pane. It rises through a turn
and falls when the agent performs a compaction. A large jump when resuming a
large session is therefore expected. The plugin reports the main thread only;
sub-agent sessions are excluded.

The sidebar shows an absolute count, not a percentage. A percentage requires a
model-to-window lookup table that silently becomes wrong as models change, and
it tends to be wrong in the confident direction. See
[ADR-0001](docs/adr/0001-absolute-tokens-not-percentage.md). Herdr 0.8.0 has no
built-in model token, and this plugin writes only `ctx` and `ctx_raw`, so there
is no supported `92k · opus` pairing in the sidebar today. The count must be
interpreted without that host-provided disambiguation.

Styling is static per sidebar slot, so values cannot change colour by magnitude;
there is no red-above-80% mode. The displayed value is intentionally compact;
use `ctx_raw` when exact arithmetic matters.

### Blank means unknown

A blank row means the plugin has no current occupancy it can stand behind. It
does not mean zero. Examples include:

- an agent restarted in place and its new session has not completed a turn;
- an agent kind whose provider is marked `In progress` in the
  [coverage table](#agent-kinds-and-provider-coverage);
- an untitled `opencode2` session, whose pane cannot be correlated safely;
- a pane whose agent session cannot be resolved; or
- the interval immediately after compaction, before a new readable occupancy
  exists.

Semantic answers like these clear a superseded value. A transient I/O failure
such as a permission or read error preserves the last value, because that value
may still describe the current session. This distinction prevents a previous
session or pre-compaction count from being presented as current while avoiding
unnecessary blanks during a temporary read failure.

## Agent kinds and provider coverage

The table below is the source of truth for provider status. An agent kind whose
provider is marked `In progress` has no current measurement: the plugin clears
`ctx` and `ctx_raw`, so its row stays blank rather than looking stale. No
provider guesses by newest-file or newest-session recency.

| Status | Agent kind | Store and occupancy | Session correlation |
| --- | --- | --- | --- |
| Implemented | `claude` | `~/.claude/projects/<dir>/<session>.jsonl`; `input_tokens + cache_read_input_tokens + cache_creation_input_tokens` | Herdr's reported `agent_session`, then an exact process-registry match, then a sole transcript under `foreground_cwd` |
| In progress | `codex` | Planned: `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`; `total_tokens`, falling back to `input_tokens` | Planned: Herdr's reported `agent_session`, then `foreground_cwd` |
| In progress | `opencode` (v1) | Planned: `~/.local/share/opencode/opencode.db`, `session` plus `message`; latest assistant `input + cache.read + cache.write` | Not shipped yet; its provider ticket must define a positive correlation method. No recency fallback. |
| In progress | `opencode2` | Planned: `~/.local/share/opencode/opencode2.db`, `session_v2` plus `session_message`; latest assistant `input + cache.read + cache.write` | Planned: pane directory plus the `OC | <session title>` terminal-title prefix; untitled sessions remain blank |

The planned opencode providers use separate stores. Their provider tickets
require read-only access; the v1 provider will not read legacy JSON message
files or the v1 database's unrelated `session_v2` tables. Herdr folds `opencode2`
into the `opencode` agent kind, so the planned dispatch also checks the
foreground process name to choose the right provider.

## Query the tokens

There is no plugin command. Herdr answers queries from its own metadata reads.
From a terminal:

```bash
herdr pane current --current | jq '.result.pane.tokens // {}'
herdr agent list | jq '.result.agents[] | {pane_id, agent, agent_status, tokens: (.tokens // {})}'
```

The exact current-pane form and token contract are also in the shipped
[SKILL.md](SKILL.md), which is intended for agents. `tokens` is absent when a
pane has no token values; an absent `ctx_raw` is unknown, never zero. To inspect
a pane returned by `herdr agent list`, use `herdr pane get <pane_id>` and read
its `.result.pane.tokens` object.

## Deliberate boundaries

This plugin reports one thing: context occupancy. It does not report or manage

- provider rate-limit windows;
- plan quota;
- dollar cost;
- session spend;
- threshold notifications; or
- update checking.

Session spend is cumulative billing across superseded turns, while context
occupancy is the current pressure on one agent session. Plan quota is measured
against a subscription time window. They are different measurements; see the
[glossary](CONTEXT.md).

## Design decisions

The implementation follows the agreed design in
[docs/design.md](docs/design.md):

- [ADR-0001: absolute tokens, not a percentage](docs/adr/0001-absolute-tokens-not-percentage.md)
- [ADR-0002: host-triggered reads, not agent hooks](docs/adr/0002-host-triggered-reads-not-agent-hooks.md)
- [ADR-0003: a dedicated plugin](docs/adr/0003-dedicated-plugin-not-upstream-contribution.md)
- [ADR-0004: provider-specific session correlation](docs/adr/0004-provider-specific-session-correlation.md)
- [ADR-0005: no plugin CLI](docs/adr/0005-no-plugin-cli.md)
