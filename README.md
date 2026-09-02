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
- an untitled `opencode` session, whose pane cannot be correlated safely;
- an `opencode` pane whose terminal title has been turned off — see
  [when an opencode row never fills in](#when-an-opencode-row-never-fills-in);
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
| Implemented | `codex` | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`; `total_tokens`, falling back to `input_tokens` | Herdr's reported `agent_session`, then the sole active TUI main thread under `foreground_cwd` |
| Implemented | `opencode` (v1) | `~/.local/share/opencode/opencode.db`, `session` plus `message`; newest completed assistant `input + cache.read + cache.write` | Herdr's reported `agent_session`, then the pane directory plus its `OC \| <session title>` terminal title |
| Implemented | `opencode2` | `~/.local/share/opencode/opencode2.db`, `session_v2` plus `session_message`; newest completed assistant `input + cache.read + cache.write` | Pane directory plus its `OC \| <session title>` terminal title; untitled sessions remain blank |
| Implemented | `pi` | `~/.pi/agent/sessions/<wrapped-cwd>/<start-timestamp>_<session-id>.jsonl`; newest completed assistant `input + cacheRead + cacheWrite`; home overridable with `PI_CODING_AGENT_DIR` | Herdr's reported `agent_session` only — a path used directly, an id matched to the sole transcript whose name ends with it, non-unique treated as unresolved; no directory or recency fallback, so pi's Herdr integration is a precondition |

The two opencode providers use separate stores, both opened read-only; the v1
provider does not read legacy JSON message files or the v1 database's unrelated
`session_v2` tables. Herdr folds `opencode2` into the `opencode` agent kind, so
dispatch also checks the foreground process name to choose the right provider.

### When an opencode row never fills in

Neither opencode version reports a session id to Herdr, so both correlate a
pane to a session through the terminal title its TUI sets: `OC | <session
title>`. That title is the only signal available, which makes four things
worth knowing before concluding the plugin is broken.

**`OPENCODE_DISABLE_TERMINAL_TITLE` turns correlation off entirely.** opencode
v1 reads it and, when it is set to `true` or `1`, never writes a terminal
title. With no title there is nothing to match, so every opencode row stays
blank for as long as the variable is set — permanently, and with no other
fallback to fall back to. The plugin cannot see the agent's environment, but it
can see its own: when it is set for the plugin too, which is the usual case
when it is exported from a shell profile, the measurement record names the
variable as the reason. opencode2 `0.0.0-next-17444` does not read this
variable, so its titles survive it.

**The TUI's own toggle does the same thing invisibly.** Both versions have a
`Disable terminal title` command in their palette, remembered across restarts.
Nothing in the environment records that it was used, so a blank opencode row
with no explanation in the plugin's output is worth checking there.

**`opencode2 mini` leaves the pane with no opencode title.** The `mini`
subcommand starts what opencode2 calls its minimal interactive interface, and
it does not set the terminal title the way the ordinary TUI does, so the title
the pane reports is the one the shell wrote for the command it is running.
Measured against opencode2 `0.0.0-next-17444`: the pane's title read
`…/github.com/alizdavoodi/herdr-context-usage` before the command and the raw
`opencode2 mini` while it ran, never an `OC | ` title, and its row stayed blank
while a pane on the same opencode2 build running the ordinary TUI reported its
occupancy normally. Herdr does still label the pane an `opencode` agent, so
this is not a pane the plugin never sees — it is one whose only correlation
signal was never written. Nothing in the pane's state announces that, so like
the palette toggle above it is invisible in the plugin's output today, and the
row stays blank permanently rather than until the agent's next turn.

**Plugin routes look like sessions.** A plugin route sets `OC | <plugin>` using
the same format, and it names no session, so a pane sitting on one is blank by
design. The plugin recognises the case when the title is longer than a session
title can be — session titles over 40 characters are shortened to 37 plus an
ellipsis, and plugin titles are never shortened — and says so rather than
reporting a session it could not find.

### When a pi row measures the previous agent session

**pi's Herdr integration does not report a session switch.** It reports the
agent session on session start and at each turn boundary, so a pi pane whose
agent session is switched inside the pane keeps reporting the previous one
until the next turn boundary — and for that interval, this plugin measures
the previous transcript instead of the one now showing in the pane.

This is documented rather than defended. The common switch resumes another
agent session in the same directory, which no directory comparison could tell
apart from the one it replaced, so there is no signal the plugin could use
instead. The interval also closes at the same turn boundary every measurement
here is already anchored to, so it is bounded by an event the numbers already
depend on, not an open-ended one.

Worth reporting upstream all the same. The integration already reports at two
other points in an agent session's life, so a switch is a third it could
report; nothing here needs the plugin to change once it does.

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
- [ADR-0006: two providers for pi and omp](docs/adr/0006-two-providers-for-pi-and-omp.md)
