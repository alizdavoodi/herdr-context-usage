# Build a dedicated plugin rather than contribute to an existing one

`senna-lang/herdr-agent-usage` already reports context meters, is MIT-licensed
Go, has substantial test coverage and drift-detection CI, and merges outside
contributions within hours. Contributing a display option and an `opencode2`
provider to it would have been less work. We are building separately anyway,
because a plugin that does one thing is easier to publish and easier for others
to adopt: the incumbent bundles context meters with provider rate limits,
pay-as-you-go spend tracking, threshold toasts and self-update, and anyone who
wants only the first has to take all five.

## Considered options

**Adopt as-is** — rejected. Its format is `⛁ 13% (130k)`, not configurable
without patching Go, and it drops the absolute count before the percentage when
the sidebar narrows, which inverts what we want. It also has no read command.

**Contribute upstream** — genuinely viable. The concerns that initially argued
against it turned out to be unfounded: it never writes `settings.json`, its
Claude and codex arithmetic are both correct, and its codex compaction handling
is better than the sibling project we were treating as the reference.

**Fork** — no advantage over building fresh once the scope is one number.

## Consequences

We inherit no test suite, no CI, and no model-drift monitoring, and must write
the provider arithmetic ourselves. That cost is bounded because the arithmetic
is documented in `docs/design.md` and, per ADR-0001, we need no model-window
table — the largest single maintenance burden in the incumbent.

Duplicated effort is real and accepted. Where a fix here would also fix a bug
there — notably that an `opencode2` pane in the incumbent silently displays a
stale opencode v1 session's number — upstreaming it is worth doing.
