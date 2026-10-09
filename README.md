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
- an untitled `opencode` session without a Herdr-reported session, whose pane
  cannot be correlated safely;
- an `opencode` pane whose terminal title has been turned off — see
  [when an opencode row never fills in](#when-an-opencode-row-never-fills-in);
- an agent home that cannot be found at all, because `HOME` is unset and no
  override points elsewhere, for a provider that needs that home to resolve the
  session: both opencode providers and `hermes` always do, while the others
  need it only when Herdr reports no absolute transcript path for the pane;
- a `hermes` pane still reporting a session Hermes has since closed — see
  [when a hermes row is blank](#when-a-hermes-row-is-blank);
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
| Implemented | `codex` | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`; `token_usage_record`, or `token_count` on rollouts that predate it; `total_tokens`, falling back to `input_tokens` | Herdr's reported `agent_session`, then the sole active TUI main thread under `foreground_cwd` |
| Implemented | `opencode` (v1) | `~/.local/share/opencode/opencode.db`, `session` plus `message`; newest completed assistant `input + cache.read + cache.write` | Herdr's reported `agent_session`, then the pane directory plus its `OC \| <session title>` terminal title |
| Implemented | `opencode2` | `~/.local/share/opencode/opencode2.db`, `session_v2` plus `session_message`; newest completed assistant `input + cache.read + cache.write` | Herdr's reported `agent_session`, then the pane directory plus its `OC \| <session title>` terminal title; untitled sessions remain blank without the former |
| Implemented | `pi` | `~/.pi/agent/sessions/<wrapped-cwd>/<start-timestamp>_<session-id>.jsonl`; newest completed assistant `input + cacheRead + cacheWrite`; home overridable with `PI_CODING_AGENT_DIR` | Herdr's reported `agent_session` only — a path used directly, an id matched to the sole transcript whose name ends with it, non-unique treated as unresolved; no directory or recency fallback, so pi's Herdr integration is a precondition |
| Implemented | `omp` | `~/.omp/agent/sessions/<stripped-cwd>/<start-timestamp>_<session-id>.jsonl`; newest completed assistant `input + cacheRead + cacheWrite`, cross-checked against the turn's own `contextSnapshot.promptTokens`; home overridable with `PI_CODING_AGENT_DIR`, the same variable that redirects pi | Herdr's reported `agent_session` only — a path used directly, an id matched to the sole transcript whose name ends with it, non-unique treated as unresolved; no directory or recency fallback, so omp's Herdr integration is a precondition for the row filling in. Herdr labels the pane an `omp` agent from its own process either way, so a missing integration shows as a blank row rather than an absent one |
| Implemented | `hermes` | `<home>/state.db` (`HERMES_HOME` overrides `~/.hermes`); `sessions.model_config._usage_anchor.prompt_tokens`, the latest main-loop call's input + cache read + cache write | Herdr's reported `agent_session` id only; no fallback, so Hermes's Herdr integration is a precondition |

The two opencode providers use separate stores, both opened read-only; the v1
provider does not read legacy JSON message files or the v1 database's unrelated
`session_v2` tables. Herdr folds `opencode2` into the `opencode` agent kind, so
dispatch also checks the foreground process name to choose the right provider.

### When an opencode row never fills in

Both opencode versions read the session Herdr's opencode integration reports
for a pane, when it reports one; `herdr integration status` shows whether the
integration is installed. From Herdr 0.9.1 (integration v12) it reports the
session an opencode2 pane has selected. Without a reported session, a pane is
correlated to a session through the terminal title its TUI sets: `OC |
<session title>`. That title is then the only signal available, which makes
four things worth knowing before concluding the plugin is broken.

**An opencode2 pane on its home screen keeps the last session's value.** The
integration reports a session when the TUI selects one and reports nothing when
it leaves for the home screen, so Herdr keeps the last session it was told
about, and the row keeps describing it until the next session is selected or
created. This is documented rather than defended: the home screen's title is
the same bare `OpenCode` an untitled session shows, so the title cannot tell the
two apart, and blanking on it would blank every untitled session the reported
id now resolves.

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
signal was never written. Unlike the palette toggle above, the plugin can say
so: the pane's process list names the `mini` subcommand, and the measurement
record reports that as the reason instead of a bare missing title. The row still
stays blank permanently rather than until the agent's next turn, because there
is no session title to correlate either way — this names the cause, it does not
recover a number.

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

### When a hermes row is blank

Measured against Hermes Agent 0.21.5, Herdr 0.9.1 and Herdr's hermes
integration v5; Hermes is not pinned to a version.

**The integration is a precondition.** A hermes pane is correlated only
through the session id Herdr's hermes integration reports, with no directory,
title or recency fallback: desktop sessions record no directory, and delegated
sub-agents run in their parent's. Without the integration the row stays blank
permanently. `herdr integration status` shows whether it is installed.

**Desktop, TUI and ACP sessions report only at start and at `/new`.** Only the
CLI reports again before each model call. So a session switched inside the app
is not seen, and the row keeps describing the session reported last. When that
session has ended, the row blanks rather than showing its last figure: Hermes
clears a session's end marker whenever it is resumed, so an ended session is
never the one live in the pane.

**Profiles are not read.** Only `<home>/state.db` is, where `<home>` is
`HERMES_HOME` or `~/.hermes`. A profile keeps its own store under
`<home>/profiles/<name>/`, and reports to Herdr only if the integration was
installed into that profile too.

**The number is a little below Hermes's own status bar.** It is the prompt size
the provider reported for the main loop's latest call. Hermes's status bar adds
that call's completion and a rough estimate of messages appended since. The row
also blanks after a compaction and before a session's first completed call,
because Hermes removes or has not yet written the figure. See
[ADR-0007](docs/adr/0007-hermes-reads-the-usage-anchor.md).

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

## Refresh mid-turn

Herdr's events fire at turn boundaries, but a long agentic turn is a run of
model calls, each with a larger prompt than the last. Between turn boundaries
the row can show the previous turn's size until something else in Herdr
happens to fire, and a turn that is filling the window fast is when the number
matters most.

The plugin declares a Herdr action, `refresh`, that runs the same refresh an
event does. Any agent hook can invoke it:

```bash
herdr plugin action invoke refresh --plugin herdr-context-usage
```

This is optional, and the plugin installs none of it: every agent has its own
hook format, and the plugin writes into no agent's configuration
([ADR-0008](docs/adr/0008-opt-in-agent-hooks-refresh-mid-turn.md)). Paste the
snippet for each agent you want refreshed per call. Without one, the row is
still correct at every turn boundary.

Each snippet does four things, and none of them is decoration:

- It returns at once unless `HERDR_ENV` is `1`, so the same agent config is
  harmless outside Herdr.
- It runs detached, so a refresh never delays the agent's next call.
- It skips sub-agents. The row reports the main thread only, and a sub-agent
  firing the hook would multiply runs for no new information.
- It invokes the full refresh rather than one pane's: the context Herdr gives
  an action names the focused pane, which need not be the pane whose hook ran.

A value read mid-turn is the occupancy of the last refresh that succeeded,
which can lag the newest call. Within one session, with no compaction since,
it is a lower bound on the call in flight; across a compaction or a session
change it is not, until a refresh observes that boundary. Concurrent
invocations are tolerated. Each write carries its refresh round's sequence number and
Herdr drops older ones, and ten simultaneous invocations ran without hitting a
plugin command limit on Herdr 0.9.3.

### Claude Code

Verified against Claude Code 2.1.293. Merge into the `hooks` object of
`~/.claude/settings.json`:

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

Claude writes each call's usage before the tool runs, so `PostToolUse` reads
the call that issued the tool. `PostCompact` clears the pre-compaction number
at once; the row fills in again when a later call records usage and a refresh
reads it. The hook's stdin JSON carries a top-level `agent_id` only inside a
sub-agent, and the guard checks only that key: a tool whose input or result
carries an `agent_id` field of its own must not silence the hook, which a plain
text match would. A missing `jq` makes the guard fail open, so sub-agents then
refresh too, which costs runs and changes nothing.

### codex

Verified against codex-cli 0.161.0. Add to `~/.codex/hooks.json`:

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

codex runs a user hook only after it is trusted. Approve it at the startup
review or through `/hooks`, and again after any edit, because trust is keyed on
the hook's content. `PostToolUse` alone is enough: the plugin reads codex's
`token_usage_record`, which is written before the tool runs, so no
`PreToolUse` pairing is needed to catch up a step.

### hermes

Verified against Hermes Agent 0.21.5. Hermes runs a hook command without a
shell, so the guard lives in a script. Save it as
`~/.hermes/agent-hooks/herdr-context-refresh.sh` and make it executable:

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

`post_api_request` fires after Hermes has written the call's usage anchor, so
this refreshes per model call, not per tool. Hermes waits for the hook's output
to close, which is why the backgrounded command redirects all three streams.
The first run asks for consent at a terminal. A session without one, such as
the gateway or ACP, skips the hook silently unless it is accepted with
`hooks_auto_accept: true` or `HERMES_ACCEPT_HOOKS=1`. A background-review fork
cannot be told apart from the main loop, so it triggers a refresh too; that
costs a run and changes nothing.

### opencode (v1)

Verified against opencode 1.18.35 and `@opencode-ai/plugin` 1.18.1. Save as
`~/.config/opencode/plugins/herdr-context-refresh.js`:

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

A sub-agent is a session with a `parentID`, which only the session events
carry; the message's own `parentID` names a message, not a session.

### opencode2

Verified against opencode 2.0.24. Its TUI loads a plugin from a directory
whose `tui.js` is the entrypoint, and it discovers every such directory under
`~/.config/opencode/plugins/` and the project's `.opencode/plugins/` on its
own. Save as `~/.config/opencode/plugins/herdr-context-refresh/tui.js`:

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

No configuration entry is needed. A directory kept elsewhere can be listed in
the `plugins` array of `~/.config/opencode/cli.json` instead; a bare file path
there is skipped without a message.

The plugin runs inside the TUI, the pane's foreground process, so it inherits
`HERDR_ENV`. `session.step.ended` is one model call and its tools, and the
assistant row it names is complete in `opencode2.db` before the event is
delivered: read from inside the handler, the row carried `time.completed` and
its tokens on every step measured. A compaction records no step, so
`session.compaction.ended` triggers too; that run finds the session
just-compacted and clears the row, which fills in again at the next step. The
row-before-event ordering was measured with the TUI on a private server
(`opencode2 --standalone`), and the per-step refreshes again with the TUI
attached to the shared background service.

### pi

Verified against pi source at `earendil-works/pi@ce950d7`. Save as
`~/.pi/agent/extensions/herdr-context-refresh.ts`:

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

pi's `turn_end` is one model call plus its tools, and it fires only after that
call's message is on disk. Handlers are awaited, so the spawn is not.

### omp

Verified against omp source at `can1357/oh-my-pi@c50daa5`. Save as
`~/.omp/agent/extensions/herdr-context-refresh.ts`:

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

omp queues the message write before `turn_end` without awaiting it, so the
ordering rests on that write being synchronous and finishing first, not on a
guarantee.

In both extensions the empty `error` handler is load-bearing: without it, a
missing `herdr` binary raises an unhandled error that can take the agent down.

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
- [ADR-0007: hermes reads the usage anchor](docs/adr/0007-hermes-reads-the-usage-anchor.md)
- [ADR-0008: opt-in agent hooks refresh mid-turn](docs/adr/0008-opt-in-agent-hooks-refresh-mid-turn.md)
