<div align="center">

# Herdr Context Occupancy

*See how full every coding agent's context window is, right in Herdr's sidebar*

[![CI](https://img.shields.io/github/actions/workflow/status/alizdavoodi/herdr-context-usage/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/alizdavoodi/herdr-context-usage/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)
[![Herdr](https://img.shields.io/badge/Herdr-%E2%89%A5%200.8.0-6e56cf?style=flat-square)](https://github.com/herdrdev/herdr)
[![Go](https://img.shields.io/badge/Go-%E2%89%A5%201.22-00add8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
![Platforms](https://img.shields.io/badge/platforms-Linux%20%7C%20macOS-555?style=flat-square)
[![Providers](https://img.shields.io/badge/providers-7-3c873a?style=flat-square)](#agent-kinds-and-provider-coverage)

[Features](#features) • [Installation](#installation) • [Usage](#usage) • [Supported agents](#agent-kinds-and-provider-coverage) • [Refresh mid-turn](#refresh-mid-turn) • [Troubleshooting](#troubleshooting)

</div>

Herdr shows you a column of running agents and their lifecycle state, but not how full any of their context windows is. That number matters to two readers. You get every agent's context at a glance in the sidebar, instead of focusing each pane and asking it. Other agents read it through the `herdr` CLI: the compressor agent, for example, can monitor every pane and coordinate compaction for those agents according to rules you define.

This [Herdr](https://github.com/herdrdev/herdr) plugin reads each agent's own session store at every turn boundary and publishes the **context occupancy** of the pane's main thread as two Herdr metadata tokens:

| Token | Example | Meant for |
| --- | --- | --- |
| `ctx` | `840`, `92k`, `1.2M` | The sidebar. Compact, and its shape varies with magnitude, so never parse it. |
| `ctx_raw` | `"92481"` | Scripts and agents. The exact integer, as a string. |

## Features

- **Seven providers** — `claude`, `codex`, `opencode` (v1), `opencode2`, `pi`, `omp` and `hermes`, each read from the agent's own store.
- **Absolute counts, not percentages** — no model-to-window lookup table that silently rots as models change.
- **Blank means unknown** — a row clears rather than show a previous session's or a pre-compaction number as current.
- **No guessing** — every pane is matched to its agent session through explicit signals, never by newest file or most recent session.
- **No daemon** — a short-lived binary that wakes on a Herdr event, does bounded work and exits.
- **Hands off your agents** — it writes into no agent's configuration and opens every store read-only.
- **Readable by other agents** — Herdr answers queries from its own commands, and the shipped [`SKILL.md`](SKILL.md) teaches an agent how.
- **Optional mid-turn refresh** — paste a hook into any supported agent to update the row after every model call.

## Installation

**Requirements**

- [Herdr](https://github.com/herdrdev/herdr) `0.8.0` or newer, on Linux or macOS.
- Go `1.22` or newer on your `$PATH`. Herdr builds the plugin from source during install and does not install toolchains itself.

```bash
herdr plugin install alizdavoodi/herdr-context-usage
```

Herdr clones the repository, shows a preview of the source in an interactive terminal, and builds the binary with the `[[build]]` command in [`herdr-plugin.toml`](herdr-plugin.toml), a CGO-free `go build`. If that build fails, installation stops with the build error. The binary is not put on your `$PATH`: Herdr runs it from the plugin root as `./herdr-context-usage`. On Herdr 0.9.4 or newer, `herdr plugin update herdr-context-usage` pulls later changes.

### From a local checkout

To run your own changes, build the checkout and link it:

```bash
cd /path/to/herdr-context-usage
CGO_ENABLED=0 go build -o herdr-context-usage ./cmd/herdr-context-usage
herdr plugin link --enabled "$PWD"
```

`plugin link` does not run the manifest's `[[build]]` command, so building is up to you, and so is rebuilding after each pull: Herdr runs whatever `./herdr-context-usage` the checkout holds. Herdr refuses to install over a linked plugin, so run `herdr plugin unlink herdr-context-usage` before switching to the GitHub install.

## Usage

### Add the sidebar row

> [!IMPORTANT]
> **Installation alone does not make a number appear.** Herdr renders a plugin token only when your own sidebar configuration names it, and this plugin deliberately does not edit `~/.config/herdr/config.toml`.

Add this to `~/.config/herdr/config.toml`:

```toml
[ui.sidebar.agents]
rows = [
  ["state_icon", "workspace", "tab"],
  ["agent", "$ctx"],
]
```

If `[ui.sidebar.agents]` already exists, add `["agent", "$ctx"]` to its `rows` list instead of adding a second table. Then reload:

```bash
herdr server reload-config
```

Focus a supported agent, or let one complete a turn, and its row fills in. The plugin refreshes on `pane.agent_status_changed`, `pane.focused`, `pane.exited` and `pane.closed`, and its `[[startup]]` hook restores tokens after a Herdr server restart. Sidebar rows are not visible in Herdr's collapsed or mobile layouts.

### Query the tokens

There is no plugin command ([ADR-0005](docs/adr/0005-no-plugin-cli.md)): the plugin writes, and Herdr answers from its own reads.

```bash
# The current pane
herdr pane current --current | jq '.result.pane.tokens // {}'

# Every agent pane
herdr agent list | jq '.result.agents[] | {pane_id, agent, agent_status, tokens: (.tokens // {})}'

# One pane from that list
herdr pane get <pane_id> | jq '.result.pane.tokens // {}'
```

Herdr omits `tokens` entirely when a pane has none, and an absent `ctx_raw` is unknown, never zero.

> [!TIP]
> For agents, [`SKILL.md`](SKILL.md) holds the same contract with fail-closed recipes: identifying your own pane, matching neighbours by `pane_id` rather than by value, and converting `ctx_raw` safely with `tonumber`.

## What the number means

Context occupancy is the number of tokens resident in the model's context window for the agent session currently owning the pane. It rises through a turn, falls when the agent compacts, and resets when a new session starts, so a large jump when resuming a large session is expected. Only the **main thread** is reported; sub-agent sessions are excluded.

It is not session spend, the cumulative billing across superseded turns, and it is not plan quota, a subscription's allowance over a time window. Those are different measurements, defined in the [glossary](GLOSSARY.md), and this plugin reports neither.

The count is absolute because a percentage needs a model-to-window table that goes stale with every release, and tends to be wrong in the confident direction ([ADR-0001](docs/adr/0001-absolute-tokens-not-percentage.md)). For the same reason there is no `92k · opus` pairing: Herdr has no built-in model token, and this plugin writes only `ctx` and `ctx_raw`. Sidebar styling is static per slot, so there is no red-above-80% mode either.

### Blank means unknown

A blank row means the plugin has no current occupancy it can stand behind. It does not mean zero. Common causes:

- an agent restarted in place, and its new session has not completed a turn;
- the interval right after a compaction, before a new readable occupancy exists;
- a pane whose agent session cannot be resolved, including an untitled `opencode` session with no Herdr-reported session;
- an agent whose Herdr integration is missing, for the providers that depend on it — see [Troubleshooting](#troubleshooting);
- no agent home at all, because `HOME` is unset and nothing [overrides](#configuration) it. Both opencode providers and `hermes` always need the home; the others need it only when Herdr reports no absolute transcript path.

A semantic answer like these clears the old value, so a previous session or pre-compaction count is never presented as current. A transient I/O failure, such as a permission or read error, keeps the last value instead, because that value may still describe the current session.

## Agent kinds and provider coverage

This table is the source of truth for which providers have shipped. An agent kind whose provider is marked `In progress` has no measurement: its tokens are cleared, so the row stays blank rather than looking stale.

| Status | Agent kind | Store and occupancy | Session correlation |
| --- | --- | --- | --- |
| Implemented | `claude` | `~/.claude/projects/<dir>/<session>.jsonl`; `input_tokens + cache_read_input_tokens + cache_creation_input_tokens` | Herdr's reported `agent_session`, then an exact process-registry match, then a sole transcript under `foreground_cwd` |
| Implemented | `codex` | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`; `token_usage_record`, or `token_count` on rollouts that predate it; `total_tokens`, falling back to `input_tokens` | Herdr's reported `agent_session`, then the sole active TUI main thread under `foreground_cwd` |
| Implemented | `opencode` (v1) | `~/.local/share/opencode/opencode.db`, `session` plus `message`; newest completed assistant `input + cache.read + cache.write` | Herdr's reported `agent_session`, then the pane directory plus its `OC \| <session title>` terminal title |
| Implemented | `opencode2` | `~/.local/share/opencode/opencode2.db`, `session_v2` plus `session_message`; newest completed assistant `input + cache.read + cache.write` | Herdr's reported `agent_session`, then the pane directory plus its `OC \| <session title>` terminal title; untitled sessions remain blank without the former |
| Implemented | `pi` | `~/.pi/agent/sessions/<wrapped-cwd>/<start-timestamp>_<session-id>.jsonl`; newest completed assistant `input + cacheRead + cacheWrite` | Herdr's reported `agent_session` only — a path used directly, an id matched to the sole transcript whose name ends with it, non-unique treated as unresolved |
| Implemented | `omp` | `~/.omp/agent/sessions/<stripped-cwd>/<start-timestamp>_<session-id>.jsonl`; newest completed assistant `input + cacheRead + cacheWrite`, cross-checked against the turn's own `contextSnapshot.promptTokens` | Herdr's reported `agent_session` only, matched the same way as `pi` |
| Implemented | `hermes` | `<home>/state.db`; `sessions.model_config._usage_anchor.prompt_tokens`, the latest main-loop call's input + cache read + cache write | Herdr's reported `agent_session` id only |

`pi`, `omp` and `hermes` have no directory, title or recency fallback, so their Herdr integration is a precondition for the row filling in. Herdr labels an `omp` pane from its own process either way, so a missing integration shows as a blank row rather than an absent one.

The two opencode providers use separate stores, both opened read-only; the v1 provider does not read legacy JSON message files or the v1 database's unrelated `session_v2` tables. Herdr folds `opencode2` into the `opencode` agent kind, so dispatch also checks the foreground process name to choose the right provider.

## Configuration

Every store location follows the agent's own override variable. The plugin reads these from its own environment, the one Herdr runs it in, not from the agent's; exported from a shell profile, they usually reach both.

| Variable | Overrides | Default |
| --- | --- | --- |
| `CLAUDE_CONFIG_DIR` | Claude Code's home | `~/.claude` |
| `CODEX_HOME` | codex's home | `~/.codex` |
| `XDG_DATA_HOME` | The directory both opencode databases sit under | `~/.local/share` |
| `OPENCODE_DB` | The opencode v1 database file | `<data home>/opencode/opencode.db` |
| `OPENCODE2_DB` | The opencode2 database file | `<data home>/opencode/opencode2.db` |
| `PI_CODING_AGENT_DIR` | The agent directory of **both** `pi` and `omp` | `~/.pi/agent` and `~/.omp/agent` |
| `HERMES_HOME` | Hermes's home | `~/.hermes` |
| `HERDR_BIN_PATH` | The `herdr` binary the plugin calls | `herdr` on `$PATH` |

> [!NOTE]
> `PI_CODING_AGENT_DIR` mirrors pi and omp themselves, which both honour that one variable, so setting it points both providers at the same directory.

## Refresh mid-turn

Herdr's events fire at turn boundaries, but a long agentic turn is a run of model calls, each with a larger prompt than the last. Between turn boundaries the row can show the previous turn's size, and a turn that is filling the window fast is exactly when the number matters most.

The plugin declares a Herdr action, `refresh`, that runs the same refresh an event does. Any agent hook can invoke it:

```bash
herdr plugin action invoke refresh --plugin herdr-context-usage
```

This is opt-in, and the plugin installs none of it: every agent has its own hook format, and the plugin writes into no agent's configuration ([ADR-0008](docs/adr/0008-opt-in-agent-hooks-refresh-mid-turn.md)). Without a hook, the row is still correct at every turn boundary.

Each snippet below does four things, and none of them is decoration:

- It returns at once unless `HERDR_ENV` is `1`, so the same agent config is harmless outside Herdr.
- It runs detached, so a refresh never delays the agent's next call.
- It skips sub-agents. The row reports the main thread only, and a sub-agent firing the hook would multiply runs for no new information.
- It invokes the full refresh rather than one pane's, because the context Herdr gives an action names the focused pane, which need not be the pane whose hook ran.

A value read mid-turn is the occupancy of the last refresh that succeeded, which can lag the newest call. Within one session, with no compaction since, it is a lower bound on the call in flight; across a compaction or a session change it is not, until a refresh observes that boundary. Concurrent invocations are tolerated: each write carries its refresh round's sequence number and Herdr drops older ones, and ten simultaneous invocations ran without hitting a plugin command limit on Herdr 0.9.3.

<details>
<summary><strong>Claude Code</strong> — verified against 2.1.293</summary>

Merge into the `hooks` object of `~/.claude/settings.json`:

```json
{
  "hooks": {
    "PostToolUse": [
      { "hooks": [ { "type": "command", "async": true,
        "command": "[ \"${HERDR_ENV:-}\" = 1 ] || exit 0; jq -e 'has(\"agent_id\")' >/dev/null 2>&1 && exit 0; herdr plugin action invoke refresh --plugin herdr-context-usage >/dev/null 2>&1 &" } ] }
    ],
    "PostCompact": [
      { "hooks": [ { "type": "command", "async": true,
        "command": "[ \"${HERDR_ENV:-}\" = 1 ] || exit 0; jq -e 'has(\"agent_id\")' >/dev/null 2>&1 && exit 0; herdr plugin action invoke refresh --plugin herdr-context-usage >/dev/null 2>&1 &" } ] }
    ]
  }
}
```

Claude writes each call's usage before the tool runs, so `PostToolUse` reads the call that issued the tool. `PostCompact` clears the pre-compaction number at once; the row fills in again when a later call records usage and a refresh reads it.

The hook's stdin JSON carries a top-level `agent_id` only inside a sub-agent, and the guard checks only that key: a tool whose input or result carries an `agent_id` field of its own must not silence the hook, which a plain text match would. A missing `jq` makes the guard fail open, so sub-agents then refresh too, which costs runs and changes nothing.

</details>

<details>
<summary><strong>codex</strong> — verified against codex-cli 0.161.0</summary>

Add to `~/.codex/hooks.json`:

```json
{
  "hooks": {
    "PostToolUse": [
      { "hooks": [ { "type": "command", "async": true,
        "command": "[ \"${HERDR_ENV:-}\" = 1 ] || exit 0; jq -e 'has(\"agent_id\")' >/dev/null 2>&1 && exit 0; herdr plugin action invoke refresh --plugin herdr-context-usage >/dev/null 2>&1 &" } ] }
    ],
    "PostCompact": [
      { "hooks": [ { "type": "command", "async": true,
        "command": "[ \"${HERDR_ENV:-}\" = 1 ] || exit 0; jq -e 'has(\"agent_id\")' >/dev/null 2>&1 && exit 0; herdr plugin action invoke refresh --plugin herdr-context-usage >/dev/null 2>&1 &" } ] }
    ]
  }
}
```

codex runs a user hook only after it is trusted. Approve it at the startup review or through `/hooks`, and again after any edit, because trust is keyed on the hook's content. `PostToolUse` alone is enough: the plugin reads codex's `token_usage_record`, which is written before the tool runs, so no `PreToolUse` pairing is needed to catch up a step.

</details>

<details>
<summary><strong>hermes</strong> — verified against Hermes Agent 0.21.5</summary>

Hermes runs a hook command without a shell, so the guard lives in a script. Save it as `~/.hermes/agent-hooks/herdr-context-refresh.sh` and make it executable:

```sh
#!/bin/sh
[ "${HERDR_ENV:-}" = 1 ] || exit 0
# Delegated sub-agents run with platform "subagent"; stdin is the hook JSON.
grep -q '"platform": "subagent"' && exit 0
herdr plugin action invoke refresh --plugin herdr-context-usage </dev/null >/dev/null 2>&1 &
```

Then add to `~/.hermes/config.yaml`:

```yaml
hooks:
  post_api_request:
    - command: "~/.hermes/agent-hooks/herdr-context-refresh.sh"
      timeout: 5
```

`post_api_request` fires after Hermes has written the call's usage anchor, so this refreshes per model call, not per tool. Hermes waits for the hook's output to close, which is why the backgrounded command redirects all three streams.

The first run asks for consent at a terminal. A session without one, such as the gateway or ACP, skips the hook silently unless it is accepted with `hooks_auto_accept: true` or `HERMES_ACCEPT_HOOKS=1`. A background-review fork cannot be told apart from the main loop, so it triggers a refresh too; that costs a run and changes nothing.

</details>

<details>
<summary><strong>opencode (v1)</strong> — verified against opencode 1.18.35 and <code>@opencode-ai/plugin</code> 1.18.1</summary>

Save as `~/.config/opencode/plugins/herdr-context-refresh.js`:

```js
import { spawn } from "node:child_process";

const subagentSessions = new Set();

export const HerdrContextRefresh = async () => {
  if (process.env.HERDR_ENV !== "1") return {};
  return {
    event: async ({ event }) => {
      const info = event.properties?.info;
      if (event.type === "session.created" || event.type === "session.updated") {
        if (info?.parentID) subagentSessions.add(info.id);
        return;
      }
      // message.updated fires repeatedly while a message streams; only a
      // completed assistant message carries the figure the plugin reads.
      if (event.type !== "message.updated" || info?.role !== "assistant") return;
      if (!info.time?.completed || subagentSessions.has(info.sessionID)) return;
      spawn("herdr", ["plugin", "action", "invoke", "refresh", "--plugin", "herdr-context-usage"],
        { detached: true, stdio: "ignore" })
        .on("error", () => {})
        .unref();
    },
  };
};
```

A sub-agent is a session with a `parentID`, which only the session events carry; the message's own `parentID` names a message, not a session.

</details>

<details>
<summary><strong>opencode2</strong> — verified against opencode 2.0.24</summary>

Its TUI loads a plugin from a directory whose `tui.js` is the entrypoint, and discovers every such directory under `~/.config/opencode/plugins/` and the project's `.opencode/plugins/` on its own. Save as `~/.config/opencode/plugins/herdr-context-refresh/tui.js`:

```js
import { spawn } from "node:child_process";

export default {
  id: "herdr.context.refresh",
  setup(api) {
    if (process.env.HERDR_ENV !== "1") return;
    const refresh = (event) => {
      // A sub-agent runs in a child session, and a child session carries a parentID.
      if (api.data.session.get(event.data.sessionID)?.parentID) return;
      spawn("herdr", ["plugin", "action", "invoke", "refresh", "--plugin", "herdr-context-usage"],
        { detached: true, stdio: "ignore" })
        .on("error", () => {})
        .unref();
    };
    // One model call and its tools; the assistant row is complete by now.
    const offStep = api.data.on("session.step.ended", refresh);
    // A compaction records no step; this clears the old number at once.
    const offCompact = api.data.on("session.compaction.ended", refresh);
    return () => { offStep(); offCompact(); };
  },
};
```

No configuration entry is needed. A directory kept elsewhere can be listed in the `plugins` array of `~/.config/opencode/cli.json` instead; a bare file path there is skipped without a message.

The plugin runs inside the TUI, the pane's foreground process, so it inherits `HERDR_ENV`. The assistant row a `session.step.ended` event names is complete in `opencode2.db` before the event is delivered: read from inside the handler, the row carried `time.completed` and its tokens on every step measured. A compaction records no step, so `session.compaction.ended` triggers too; that run finds the session just-compacted and clears the row, which fills in again at the next step. The ordering was measured with the TUI on a private server (`opencode2 --standalone`), and the per-step refreshes again with the TUI attached to the shared background service.

</details>

<details>
<summary><strong>pi</strong> — verified against <code>earendil-works/pi@ce950d7</code></summary>

Save as `~/.pi/agent/extensions/herdr-context-refresh.ts`:

```ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { spawn } from "node:child_process";

export default function (pi: ExtensionAPI) {
  if (process.env.HERDR_ENV !== "1") return;
  pi.on("turn_end", (_event, ctx) => {
    // Sub-agents run as headless `pi --mode json` children; only the TUI owns the pane.
    if (ctx.mode !== "tui") return;
    spawn("herdr", ["plugin", "action", "invoke", "refresh", "--plugin", "herdr-context-usage"],
      { detached: true, stdio: "ignore" })
      .on("error", () => {})
      .unref();
  });
}
```

pi's `turn_end` is one model call plus its tools, and it fires only after that call's message is on disk. Handlers are awaited, so the spawn is not.

</details>

<details>
<summary><strong>omp</strong> — verified against <code>can1357/oh-my-pi@c50daa5</code></summary>

Save as `~/.omp/agent/extensions/herdr-context-refresh.ts`:

```ts
import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";
import { spawn } from "node:child_process";

// A nested `omp` started from a parent session's shell inherits OMPCODE=1.
const nested = process.env.OMPCODE === "1";

export default function (pi: ExtensionAPI) {
  if (process.env.HERDR_ENV !== "1" || nested) return;
  pi.on("turn_end", (_event, ctx) => {
    // In-process sub-agents and the advisor report a kind other than "main".
    if (ctx.agent.kind !== "main") return;
    spawn("herdr", ["plugin", "action", "invoke", "refresh", "--plugin", "herdr-context-usage"],
      { detached: true, stdio: "ignore" })
      .on("error", () => {})
      .unref();
  });
}
```

omp queues the message write before `turn_end` without awaiting it, so the ordering rests on that write being synchronous and finishing first, not on a guarantee.

</details>

> [!CAUTION]
> In every JavaScript and TypeScript snippet, keep the empty `error` handler: without it, a missing `herdr` binary raises an unhandled error that can take the agent down.

## Troubleshooting

`herdr integration status` shows which of Herdr's agent integrations are installed, which is the first thing to check for a row that stays blank.

### When an opencode row never fills in

Both opencode versions read the session Herdr's opencode integration reports for a pane, when it reports one; from Herdr 0.9.1 (integration v12) that includes the session an opencode2 pane has selected. Without a reported session, a pane is correlated through the terminal title its TUI sets, `OC | <session title>`, and that title is then the only signal available.

- **An opencode2 pane on its home screen keeps the last session's value.** The integration reports a session when the TUI selects one and nothing when it leaves for the home screen, so the row keeps describing the last session until the next one is selected or created. This is documented rather than defended: the home screen's title is the same bare `OpenCode` an untitled session shows, so blanking on it would blank every untitled session the reported id now resolves.
- **`OPENCODE_DISABLE_TERMINAL_TITLE` turns correlation off.** When opencode v1 sees it set to `true` or `1`, it never writes a title, so every opencode v1 row without a reported session stays blank for as long as it is set. When the plugin's own environment has it too, as it usually does when exported from a shell profile, the measurement record names the variable as the reason. opencode2 `0.0.0-next-17444` does not read it.
- **The TUI's own `Disable terminal title` command does the same thing invisibly.** Both versions remember it across restarts and nothing in the environment records it, so a blank opencode row with no explanation in the plugin's output is worth checking there.
- **`opencode2 mini` never sets an opencode title.** The pane keeps the title the shell wrote for the command, so its row stays blank. The plugin can tell from the pane's process list, and the measurement record names `mini` as the reason instead of a bare missing title. That names the cause; it does not recover a number.
- **Plugin routes look like sessions.** A plugin route sets `OC | <plugin>` in the same format but names no session, so a pane sitting on one is blank by design. The plugin recognises it when the title is longer than a session title can be (session titles over 40 characters are shortened to 37 plus an ellipsis; plugin titles never are) and says so.

### When a pi row measures the previous agent session

pi's Herdr integration reports the agent session at session start and at each turn boundary, but not when the session is switched inside the pane. Until that pane's next turn boundary, the row measures the previous transcript rather than the one now showing.

This is documented rather than defended. The common switch resumes another session in the same directory, which no directory comparison could tell apart, and the interval closes at the same turn boundary every reading here is already anchored to. A session-switch report from the integration upstream would close it, with no change needed here.

A `pi` or `omp` row that is blank permanently usually means its Herdr integration is not installed, since neither has a fallback.

### When a hermes row is blank

Measured against Hermes Agent 0.21.5, Herdr 0.9.1 and Herdr's hermes integration v5; Hermes is not pinned to a version.

- **The integration is a precondition.** A hermes pane is correlated only through the session id the integration reports: desktop sessions record no directory, and delegated sub-agents run in their parent's, so there is no safe fallback.
- **Desktop, TUI and ACP sessions report only at start and at `/new`.** Only the CLI reports again before each model call, so a session switched inside the app is not seen. When the session reported last has ended, the row blanks rather than showing its last figure: Hermes clears a session's end marker whenever it is resumed, so an ended session is never the one live in the pane.
- **Profiles are not read.** Only `<home>/state.db` is. A profile keeps its own store under `<home>/profiles/<name>/`, and reports to Herdr only if the integration was installed into that profile too.
- **The number is a little below Hermes's own status bar.** It is the prompt size the provider reported for the main loop's latest call; Hermes's status bar adds that call's completion and an estimate of messages appended since. The row also blanks after a compaction and before a session's first completed call, because Hermes removes or has not yet written the figure ([ADR-0007](docs/adr/0007-hermes-reads-the-usage-anchor.md)).

## Scope

This plugin reports one thing: context occupancy. A plugin that computes one number is easier to trust and cheap to adopt without inheriting opinions you did not ask for, so it deliberately does not report or manage:

- provider rate-limit windows;
- plan quota;
- dollar cost or session spend;
- threshold notifications;
- update checking.

## Development

```bash
go test ./...
CGO_ENABLED=0 go build -o herdr-context-usage ./cmd/herdr-context-usage
```

[CI](.github/workflows/ci.yml) runs `go vet`, the manifest's build and the tests on Linux and macOS, against the Go version in `go.mod`, so a green run also means `herdr plugin install` builds on the oldest supported Go.

The build is CGO-free because both opencode databases are read through the pure-Go [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite), which keeps the binary static. One package per provider lives under [`internal/provider/`](internal/provider), [`internal/herdr/`](internal/herdr) wraps the Herdr CLI, and [`internal/app/`](internal/app) runs a refresh round.

Start with these before changing behaviour:

- [GLOSSARY.md](GLOSSARY.md) fixes the vocabulary: occupancy, agent session, main thread, resolution mode.
- [docs/design.md](docs/design.md) and [docs/design-pi-omp.md](docs/design-pi-omp.md) record the agreed design.
- The architecture decision records explain the choices that look surprising:
  - [ADR-0001: absolute tokens, not a percentage](docs/adr/0001-absolute-tokens-not-percentage.md)
  - [ADR-0002: host-triggered reads, not agent hooks](docs/adr/0002-host-triggered-reads-not-agent-hooks.md)
  - [ADR-0003: a dedicated plugin](docs/adr/0003-dedicated-plugin-not-upstream-contribution.md)
  - [ADR-0004: provider-specific session correlation](docs/adr/0004-provider-specific-session-correlation.md)
  - [ADR-0005: no plugin CLI](docs/adr/0005-no-plugin-cli.md)
  - [ADR-0006: two providers for pi and omp](docs/adr/0006-two-providers-for-pi-and-omp.md)
  - [ADR-0007: hermes reads the usage anchor](docs/adr/0007-hermes-reads-the-usage-anchor.md)
  - [ADR-0008: opt-in agent hooks refresh mid-turn](docs/adr/0008-opt-in-agent-hooks-refresh-mid-turn.md)
