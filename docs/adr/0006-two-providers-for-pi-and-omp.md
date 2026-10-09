# Two providers for pi and omp, not one reader with two homes

omp is built on pi, and the difference between them presents itself as the
directory each keeps its sessions in: `~/.pi/agent` against `~/.omp/agent`. That
framing invites one reader parameterised by a home path. We measured both stores
instead, and the home turns out to be the one thing they genuinely share — both
honour `PI_CODING_AGENT_DIR`, so a single exported variable redirects either
agent. Everything a reader actually touches diverges.

The working-directory slug differs. pi wraps the whole absolute path, filing
`/Users/alirezadavoodi` as `--Users-alirezadavoodi--`; omp strips the home
prefix, filing this repository as
`-projects-github.com-alizdavoodi-herdr-context-usage`. The per-turn usage
record differs: pi carries `usage.reasoning` and no `contextSnapshot`, omp
carries `contextSnapshot.promptTokens` and `nonMessageTokens` and no
`reasoning`. Model changes differ: pi writes `provider` plus `modelId`, omp
writes `model` plus `role`. Both stamp `version: 3` on the session record, so
the schema version cannot be used to tell them apart. Their release lines are
unrelated, pi 0.84.2 against omp 17.3.5.

The compaction records settle it. pi's carries its own `usage`, the cost of
generating the summary, where omp's carries none; pi writes `fromHook` where omp
writes `fromExtension`. One reader that walks back to the newest record holding
a `usage` is correct on omp and silently wrong on pi, where it reports 1383
tokens — plausible for a freshly compacted session, and therefore the kind of
wrong number nobody questions. Two readers, each keying on its own record type,
cannot make that mistake.

`opencodeshared` is the precedent for what may still be shared. Extraction is
allowed where the behaviour was measured identical on both, as the occupancy sum
`input + cacheRead + cacheWrite` was against cached turns on each agent
independently, and refused where it was not.

## Consequences

The two packages look near-identical at a glance, so merging them will keep
presenting itself as an obvious simplification. The divergences above are the
answer, and every one of them was established by reading real transcripts from
both agents rather than inferred from their shared ancestry.

`version: 3` on both session records is an active trap. It reads as a
compatibility signal and is not one — the field sets already differ at that
version, so gating on it would reject a harmless bump while accepting a breaking
change within one.

Each provider declares its own additional superseding outcomes through
`Superseded()`, while the shared lifecycle outcomes — no session, no home, no
occupancy and just-compacted — are classified once in `provider.IsSuperseded`.
The seam test derives its list of providers from the registry rather than a
hand-written one, so a newly registered provider is reached without a second
list to keep in step, and the Measure, root and refresh tests cover the shared
contract end to end.

Neither pi nor omp is ours, and `nix run github:numtide/llm-agents.nix` pins
neither, so both float on whatever the flake resolves to. Every measurement
behind this decision therefore carries the version it was taken against, the way
the README already names the opencode2 build whose behaviour it describes.
