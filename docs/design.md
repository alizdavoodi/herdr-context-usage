# Design: herdr context usage

Status: agreed. Which providers have actually shipped is the coverage table in
[README.md](../README.md#agent-kinds-and-provider-coverage) and nowhere else;
this document records the design. Terminology is defined in
[CONTEXT.md](../CONTEXT.md).

## Why this exists

herdr shows you a column of running agents and their lifecycle state, but not
how full any of them is. The number that decides whether you paste a large file
into a pane, or clear it first, is invisible — you have to focus each agent and
ask it.

herdr will not provide this. Its agent model is semantic lifecycle state only
(`idle`/`working`/`blocked`/`done`/`unknown`), and its docs say plainly: *"Report
display-only values separately through metadata."* Three requests for token or
context telemetry — herdrdev/herdr#2742, #2437, #1608 — were closed
`NOT_PLANNED`. There is no token, context, usage or compaction concept anywhere
in the v0.8.0 CLI, socket API, JSON schema, or config. This absence is a
deliberate boundary, not a gap awaiting a patch, so the number has to come from
outside herdr and be pushed in.

## Scope

**In.** Context occupancy for `claude`, `codex`, `opencode` (v1) and `opencode2`,
shown in the agent sidebar and readable by other agents. The `pi` and `omp`
agent kinds are specified separately in
[design-pi-omp.md](design-pi-omp.md), which extends this document rather than
replacing it. The sections below were written for the four providers shipped
first and gained an entry for each of those two as its provider shipped, so
both now appear among them while the **In** list above still names four. That
list records what this document was written to cover, not what has shipped; the
coverage table named in the Status line is the only statement of the latter.

**Out**, and each of these is a deliberate no: provider rate-limit windows, plan
quota, dollar cost, session spend, threshold notifications, update checking.

The single-purpose boundary is the point. Five plugins already report some
flavour of usage and the most mature bundles context meters, rate limits,
pay-as-you-go spend, toasts and self-update into one artifact. A plugin that
computes one number is easier to publish, easier to trust, and cheap for someone
else to adopt without inheriting opinions they did not ask for. It also keeps us
inside one of herdr's 32 per-pane reporter slots and two of its 32 token keys,
leaving the rest of the budget for other tools.

## Architecture

```
herdr emits pane.agent_status_changed / pane.focused / pane.exited / pane.closed
  └─> plugin binary (short-lived, one process per event)
        ├─ herdr agent list                    → live agents, pane ids, cwds
        ├─ resolve session per pane            → resolution mode recorded
        ├─ provider reads the agent's own store → occupancy
        └─ herdr pane report-metadata --token  → ctx, ctx_raw

any coding agent, any pane
  └─> herdr agent list | jq                    → reads the same tokens back
```

Three properties fall out of this shape and each was chosen rather than
inherited:

**No process of ours runs continuously.** herdr has no daemon concept — its
`[[startup]]` hook is documented as *"one-shot initialization commands, not
supervised daemons"* — so the plugin is a binary that wakes on an event, does
bounded work, and exits.

**Nothing of ours is installed into any agent's configuration.** We do not write
`settings.json`, `config.toml`, or any plugin directory belonging to Claude,
codex or opencode. See [ADR-0002](adr/0002-host-triggered-reads-not-agent-hooks.md).

**The plugin has no query interface.** Because herdr returns metadata tokens
from its own read commands, the plugin writes and herdr answers. See
[ADR-0005](adr/0005-no-plugin-cli.md).

### Trigger

`pane.agent_status_changed` is a turn boundary, which is exactly when occupancy
changes and when the number becomes actionable. `pane.focused` covers the case
where you look at a pane whose agent has been quiet.

`pane.exited` and `pane.closed` cover the other side of that lifecycle. They
trigger the same all-agent refresh. Each refresh also compares `herdr pane list`
with `herdr agent list` and clears every non-agent pane that still exposes a
context-occupancy token, because Herdr does not clear source-scoped metadata
when an agent is released. The event pane remains a fallback if pane enumeration
races or fails.

Every event refreshes **all** live agents, not only the event's pane. This looks
wasteful and is deliberate: herdr's detection rules for opencode match the string
`"esc to interrupt"`, and opencode2 renders `"interrupt"` / `"again to
interrupt"` instead, so an opencode2 pane may never emit a status change at all.
Refreshing everything means an unreliable pane still updates whenever any of its
neighbours transitions. Reads are bounded and cheap enough that the fan-out is
affordable.

A `[[startup]]` hook re-reads everything once, because herdr documents that
*"Token metadata is not restored after a server restart."* Without it, a restart
blanks every row until each agent independently takes another turn — possibly
hours. This is correctness, not polish.

### Providers

The extraction arithmetic differs per agent and the differences are not
guessable. Each of these was measured, and two of them punish the obvious
assumption.

| Provider | Store | Occupancy |
|---|---|---|
| `claude` | `~/.claude/projects/<dir>/<session>.jsonl`, bounded reverse scan | `input_tokens + cache_read_input_tokens + cache_creation_input_tokens` |
| `codex` | `~/.codex/sessions/YYYY/MM/DD/rollout-*-<session>.jsonl` | `last_token_usage.total_tokens`, falling back to `input_tokens` |
| `opencode` | SQLite `~/.local/share/opencode/opencode.db`, `session` + `message` | `tokens.input + tokens.cache.read + tokens.cache.write` |
| `opencode2` | SQLite `~/.local/share/opencode/opencode2.db`, `session_v2` + `session_message` | same as `opencode` |
| `pi` | `~/.pi/agent/sessions/<wrapped-cwd>/<start-timestamp>_<session-id>.jsonl`, bounded reverse scan | the same three addends as `claude` under pi's own field names, which the [coverage table](../README.md#agent-kinds-and-provider-coverage) spells out |
| `omp` | `~/.omp/agent/sessions/<stripped-cwd>/<start-timestamp>_<session-id>.jsonl`, bounded reverse scan | those same three addends, spelled as pi spells them, cross-checked against the turn's own prompt-size field and never sourced from it; the [coverage table](../README.md#agent-kinds-and-provider-coverage) spells both out |

**Claude's three cache fields are disjoint segments of one prefix**, so they add.
This also matches Claude Code's own internal `total_input_tokens`, which means
our number and the agent's own cannot drift apart.

**Codex's `cached_input_tokens` is a subset of `input_tokens`, not an addend.**
Adding them roughly doubles the answer — measured at 84,736 cached of 85,751
input, a 1.99x overstatement that is plausible enough to go unnoticed. Nothing
sums them.

**Codex reports `total_tokens` rather than `input_tokens`** because of
compaction. On the turn a compact occurs, codex zeroes the entire breakdown and
carries the true post-compaction size only in `total_tokens`:

```
seq 395  {input 245400, cached 238336, output 315, total 245715}
seq 396  compacted
seq 399  {input 0, cached 0, output 0, total 18162}
```

Reading `input_tokens` there yields `0`; refusing zero and walking back yields
`245400`. Both are wrong when the answer is `18162` — the difference between
displaying 95% full and 7% full. Preferring `total_tokens` costs one turn's
output tokens, measured at 0.9%, and is the right trade.

**opencode2 was verified, not assumed.** Its plugin API is undocumented and
shifting, and a sibling project's design notes asserted parity without recording
the arithmetic. Consecutive assistant rows from a live session settle it:

```
input 15458, cache.read 0      → 15458
input  2445, cache.read 14848  → 17293
input 14829, cache.read 16896  → 31725
```

`input` (2,445) is smaller than `cache.read` (14,848) at the second turn, which
is impossible if cache were a subset. Cache is normalised out of input; the sums
climb monotonically. The v1 formula applies unchanged.

**Never use `session_v2.tokens_input` and its siblings.** Those columns look
exactly like what you want and are cumulative accumulators that ignore
compaction — measured rising across a compact. They are session spend wearing
the right field names.

**pi's compaction record carries a `usage` of its own** — the cost of generating
the summary. A walk that stopped at the newest record holding a `usage` would
therefore read the compaction record itself and report 1,383 tokens, entirely
plausible for a freshly compacted agent session and so the kind of wrong number
nobody questions. Detection keys on the record type, never on the presence of a
`usage`. Measured against pi 0.84.2, which is not pinned. This shape is also one
of the divergences [ADR-0006](adr/0006-two-providers-for-pi-and-omp.md) rests
on: omp's compaction record carries no `usage`, so one reader serving both agents
would be correct on omp and silently wrong here.

**omp's hazard runs the other way.** Carrying no `usage`, omp's compaction
record cannot lure a walk into stopping on the record itself; the failure
available to omp is walking straight past it to the last pre-compaction turn,
measured at a 3.2x overstatement — a nearly full window reported for an agent
session that is a third full. pi fails low and plausible, omp fails high and
plausible, and a walk keyed on anything but the record type is wrong on one of
them. Measured against omp 17.3.5, which is not pinned.

### Resolving which session a pane is on

Getting this wrong produces a confident wrong number, which is worse than a blank
one, so resolution is per-provider and every measurement records which mode
produced it.

1. **`agent_session`** — herdr's own integrations report a native session id and
   path per pane. Exact. Requires `herdr integration install <kind>`, which is
   additive hook scripts and does not disturb an existing Claude statusLine.
   For every provider a reported path is used only when it is absolute: a
   relative one would be read against whatever directory herdr launched this
   plugin in, where a same-named file is not the pane's transcript, so it is
   refused.
2. **Provider-specific fallback** when no integration has reported:
   - `claude` — first require an exact PID, working-directory and agent-kind
     match in Claude's process registry (`process_registry`); if no registry
     entry matches, accept a sole transcript under the encoded
     `foreground_cwd` (`foreground_cwd`).
   - `codex` — derive from the pane's `foreground_cwd`, accepting only an active
     TUI main thread. `exec` and TUI rollouts are otherwise indistinguishable,
     and 159 of 307 rollouts measured here were `exec`.
   - `opencode` (v1) — match the pane's `terminal_title`, exactly as v2 does.
     This was not in the original design, which left v1 with no fallback at all
     and therefore inert, since no integration reports `agent_session`. Reading
     the v1 binary settled it: v1 emits the same `OC | <session title>` format
     with the same truncation, and title plus directory is unique across all
     2160 v1 sessions on this machine.
   - `opencode2` — match the pane's `terminal_title`. Its TUI sets the title to
     `OC | <session title>`, stock behaviour from the binary itself, and herdr
     surfaces OSC titles in `terminal_title`. Prefix-match, because titles over
     40 characters are truncated to `slice(0,37)+"..."`, or `slice(0,37)+"…"`
     from opencode 1.18.32 and opencode2 2.0.18. Since herdr 0.9.1's opencode
     integration (v12), an opencode2 pane usually reaches step 1 instead, and
     this is the fallback for panes without it.

Verified end to end against a live opencode2 pane inside herdr: the pane
reported `terminal_title: "OC | Probe test response"`, and stripping the prefix
and matching on `directory` plus a title prefix resolved to exactly the expected
session id, yielding its occupancy. The whole opencode2 path is confirmed
working, not merely plausible.
3. **Blank** when neither resolves.

`pi` stops at step 1, and its missing step 2 is deliberate rather than
unfinished. It resolves from herdr's reported agent session and nothing else: a
path used directly, an id matched against the transcript whose name ends with it
under pi's home, and a match that is not unique treated as unresolved rather
than chosen between. A working-directory fallback was considered and refused,
because pi files an agent session under the directory it *started* in, so a pane
that has since moved resolves to nothing or to an unrelated agent session, and
because the transcript does not mark a sub-agent session, so a directory-scoped
match would have nothing to exclude one with. Installing pi's herdr integration
is therefore a precondition for the row filling in at all, not an improvement on
a fallback that works without it. Observed against pi 0.84.2 and herdr 0.8.0;
pi is not pinned.

`omp` stops at step 1 as well, and for the same reasons rather than for one of
its own. An earlier revision claimed a third that was omp's alone — no
screen-detection manifest, so no agent label without the integration — and
[ADR-0004](adr/0004-provider-specific-session-correlation.md) records why that
was withdrawn: herdr 0.8.0 with omp 18.1.4 labels an omp pane from the process
name, so the pane reaches this plugin with or without the integration and
simply resolves to nothing without it. Both integrations therefore decide the
same thing, which is whether a pane herdr already calls an agent can be
correlated at all. What it resolves *with* takes the same shape — a path used
directly, an id matched against the transcript whose name ends with it under
omp's own home, a non-unique match treated as unresolved — but the divergence
is beneath that home, not the home itself. The directory derivation is omp's
own, pi wrapping the whole absolute path where omp strips the home prefix. The
configured override reaches each provider unresolved, so leaving it unset keeps
omp on its own default home rather than pointing an omp pane at pi's store on a
machine running both. One exported `PI_CODING_AGENT_DIR` still redirects both
agents; that override is the one thing they genuinely share, and
[ADR-0006](adr/0006-two-providers-for-pi-and-omp.md) records the divergences
that survive it. Observed against omp 17.3.5 and herdr 0.8.0; omp is not
pinned.

Title correlation carries a limitation worth stating plainly, because it makes a
row blank forever rather than briefly. Both opencode versions can switch their
terminal title off: v1 honours `OPENCODE_DISABLE_TERMINAL_TITLE`, and both expose
an undocumented palette command that persists to `~/.config/opencode/tui.json`.
With no title there is no fallback — herdr reports no session for these panes and
recency is banned — so the row blanks permanently and looks exactly like an agent
that has not taken a turn. The environment variable is detected; the palette
toggle is not, until the plugin reads that file.

Naive recency is banned outright. On the only live opencode2 sample available,
three methods gave three different answers and the pane was on the *third*-most
recent session in its directory. Directory collision is severe — 14 sessions
share one repository, 13 another. See
[ADR-0004](adr/0004-provider-specific-session-correlation.md).

Herdr's `agent` field is the primary provider signal for `claude`, `codex`,
`pi` and `omp`, the last two of which Herdr reports as their own agent kinds
rather than folding them into another, so their dispatch needs no process
inspection either. Herdr derives the `omp` label from the pane's own process,
which is why it is present whether or not omp's integration is; observed on
herdr 0.8.0 with omp 18.1.4. `opencode2` panes additionally need distinguishing
from `opencode` ones because Herdr folds the kind. For an `opencode` agent,
compare every foreground process's `argv0` basename case-insensitively after
stripping a leading dot, and choose `opencode2` if any such process is present;
process-list order is not meaningful. An unavailable process list leaves the
provider undetermined rather than defaulting to `opencode`; a successfully
observed empty list selects `opencode`.

### Reporting

Two metadata tokens per pane, one stable source id for the plugin's whole
lifetime — herdr consumes a source slot permanently and *"clearing or expiry does
not release those source slots."*

| Token | Value | For |
|---|---|---|
| `ctx` | `92k` | the sidebar |
| `ctx_raw` | `92481` | machines |

`ctx_raw` exists because herdr token values are always strings, so an agent
reading `ctx` would have to parse a display format that varies between `840`,
`92k` and `1.2M`, losing precision. One extra key against a 16-per-report budget
buys exact integers.

No TTL. An idle agent's occupancy has genuinely not changed, so an hours-old
value is still true; expiring it would delete correct data to guard against a
failure better handled by clearing explicitly when an agent exits.

Unknown is expressed by writing nothing. herdr's renderer does the rest: *"a row
disappears when none of its tokens have a value."* Never zero, never a stale
value dressed as live, and — during just-compacted — never the pre-boundary
number.

### Display

The sidebar shows absolute tokens, `92k`, not a percentage. See
[ADR-0001](adr/0001-absolute-tokens-not-percentage.md).

herdr renders a plugin token only if the user names it in their own config, and
this plugin will not edit that file:

```toml
[ui.sidebar.agents]
rows = [
  ["state_icon", "workspace", "tab"],
  ["agent", "$ctx"],
]
```

Verified constraints: token names match `^[A-Za-z0-9_-]{1,32}$`, values cap at
80 characters, at most 16 tokens per row and 16 rows per layout. Styling accepts
`fg` as strict `#RGB`/`#RRGGBB` plus `bold`/`dim`, but is **static per slot** —
there is no way to colour a value by its magnitude, so no red-above-80%.
Sidebar rows are also invisible in the collapsed and mobile layouts.

### Query

No custom command. herdr returns metadata tokens from its own reads:

```bash
herdr pane current --current | jq '.result.pane.tokens'
herdr agent list | jq '.result.agents[] | {pane_id, agent, agent_status, tokens}'
```

Note there is no `--json` flag; these emit JSON unconditionally. The `tokens`
key is absent rather than empty when a pane has none, so consumers should use
`.tokens // {}`.

A `SKILL.md` ships with the plugin documenting these two commands and the token
names, so an agent can discover the capability. None of the eleven plugins
surveyed ships one, and herdr's own agent skill says nothing about tokens.

## Lifecycle edge cases

| Situation | Behaviour |
|---|---|
| Agent restarts in place | Blank until the new session produces a turn |
| `--resume` of a large session | Jumps to the resumed size |
| Sub-agents | Excluded; main thread only |
| Compaction | Number drops — a correctness requirement on the provider |
| Just-compacted | Report the state, not `0` and not the previous number |
| herdr server restart | `[[startup]]` re-reads everything |
| Agent exits | Tokens cleared explicitly; Herdr does not clear them on agent release |
| Pane moved across workspaces | Pane id changes; re-resolve rather than caching ids |

## Risks and unverified assumptions

1. **herdr folds `opencode2` into `opencode`** via a hardcoded trailing-`2`
   strip. A local manifest override introducing a new kind is silently ignored,
   and `rows_by_agent.opencode2` is rejected. We disambiguate ourselves; there is
   no upstream fix available.
2. **opencode2 may never report `working`**, so its rows depend on the
   refresh-everything fan-out rather than its own events.
3. **Untitled opencode2 sessions cannot be resolved** — roughly 10% render a bare
   `OpenCode` title. Those panes stay blank by design unless Herdr reports
   their session.
4. **opencode's schema is mid-migration.** The v1 database contains both
   `session`/`message` and a stalled `session_v2`. Version-probe; do not pin.
5. ~~Unverified: that herdr surfaces the opencode2 OSC title in
   `terminal_title`.~~ **Verified** against a live opencode2 pane in herdr —
   `OpenCode` on the home screen, `OC | <session title>` on a session, and
   `pane process-info` reporting `argv0: "opencode2"` while herdr's `agent`
   field reads `opencode`, confirming both the title channel and the version
   discriminator.
6. ~~Unverified: whether herdr clears source-scoped tokens when an agent is
   released.~~ **Verified against Herdr 0.8.0:** a temporary pane was given a
   synthetic `claude` agent, one ordinary metadata token, and one token scoped
   with `--agent claude --applies-to-source <agent-source>`. Before release,
   `herdr pane get` returned both tokens. After `herdr pane release-agent`, the
   pane's agent status became `unknown` but `pane get` still returned both token
   values. The plugin must therefore clear `ctx` and `ctx_raw` explicitly from
   every pane that no longer appears in `herdr agent list`.
7. Any percentage feature would reintroduce a model-to-window lookup table that
   rots with every model release. Codex supplies `model_context_window` for free;
   Claude and opencode do not.

## Implementation notes

Go, with `modernc.org/sqlite` so both opencode databases are readable from a
CGO-free static binary. `[[build]]` runs `go build`; a release-asset fallback
becomes worthwhile once there are tagged releases, since reinstall is the only
update path herdr offers and a failed build aborts the install.

Two rules to carry into every provider, both learned by measurement elsewhere:
never overwrite a good value with zero, and represent unknown as null rather
than zero in any JSON — `jq` cannot distinguish an absent key from a null one,
and zero is a legitimate reading.
