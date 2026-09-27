# hermes reads the usage anchor, and an ended session blanks

Hermes keeps every session in one SQLite store, `state.db`, and several things
in it look like a token count. Only one is context occupancy.
`sessions.model_config._usage_anchor.prompt_tokens` is the provider-reported
prompt size of the main loop's latest API call, Hermes's own
`input + cache_read + cache_write`. Hermes rewrites it after every call and
removes it when a compaction or a mismatched resume makes it stale. Measured
against Hermes Agent 0.21.5 (schema version 30), Herdr 0.9.1 and Herdr's hermes
integration v5, the live values matched the `in=` figure `agent.log` printed
for the same call exactly.

The alternatives each fail on a fact of the store:

- **The cumulative columns.** `sessions.input_tokens`, the `cache_*_tokens`
  columns and `session_model_usage` sum every call in the session. That is
  session spend, not occupancy, and compaction does not lower it.
- **`messages.token_count`.** It is NULL on every row. Nothing in Hermes writes
  it.
- **`agent.log`.** It rotates, and the background-review fork logs under its
  parent's session id, so a line cannot be attributed to the main thread.
- **A new Hermes hook.** `post_api_request` would carry the same figure, but it
  means a second plugin for the user to install beside Herdr's. That is worse
  than reading a store Hermes already writes.

The anchor is absent before a session's first completed call, after a
compaction, and after a resume whose history no longer matches it. The row
blanks in each case rather than falling back to one of the figures above. A
compacted message row separates the compaction case, recorded as
just-compacted, from the rest.

**A present anchor is not trusted on its own.** In-place compaction commits
the new transcript first and clears the anchor afterwards, in a separate
best-effort write whose failure Hermes swallows, so an old anchor can sit
beside a compacted transcript. Hermes guards against this by checking that the
anchor still matches the transcript. The plugin mirrors the first half of that
check: an anchor is read only while the session has at least `base_count`
active message rows, and fewer means just-compacted. On every anchored live
session, the active row count was exactly `base_count + 1`. Hermes also
flushes pending messages before each tool runs, so a live session falls below
`base_count` only briefly after an anchor write, and then the row blanks
rather than shows a wrong figure. A compaction that leaves `base_count` or more
active rows is not caught. The full check would compare Hermes's
`base_last_fp` fingerprint, which means reproducing Python's `json.dumps`
byte for byte in Go, so it was left out.

**An ended session blanks.** Herdr's integration reports a desktop, TUI or ACP
session only at start and at `/new`, so the pane keeps reporting a session
after the user has switched away or closed it. The measured desktop pane was
still reporting a session closed two days earlier, and its anchor was still a
real figure. Every Hermes resume path clears `ended_at` again, so a row with
`ended_at` set is never the session live in the pane. Showing its anchor would
be a confident number for the wrong session. The one exception is
`end_reason = 'compression'`. That is legacy rotating compaction, where a child
row takes over, so it is recorded as just-compacted rather than ended.

Out of scope: Hermes profiles, tracking desktop session switches, a window limit
or percentage ([ADR-0001](0001-absolute-tokens-not-percentage.md)), and
shipping or changing any Hermes or Herdr integration.
