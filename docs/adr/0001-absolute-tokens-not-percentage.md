# Show absolute tokens, not a percentage

Every comparable plugin renders context usage as a percentage, usually with a
gauge glyph. We show an absolute count — `92k` — because a percentage requires
knowing each model's window limit, and that requires a model-to-window lookup
table that silently rots: leave a model that shipped with a 1M window pinned at
200k and the displayed percentage is five times too large, wrong in the
confident direction, with nothing to catch it. Codex hands over
`model_context_window` for free, but Claude and opencode do not, so a percentage
would mean maintaining that table forever for a number the raw count already
conveys.

## Consequences

The same digits mean different things on adjacent rows — `92k` is comfortable on
a 200k window and dire on a 128k one. Herdr 0.8.0 has no built-in model token,
and this plugin writes only `ctx` and `ctx_raw`, so there is no supported way to
pair the count with a model in the sidebar today. That ambiguity is accepted
rather than introducing a model lookup table or pretending the host provides a
mitigation.

herdr's sidebar styling is static per slot and cannot vary by value, so a
severity colour was never available anyway. Adding a percentage later means
adding the table and its maintenance, not rewriting the extractors.

omp is a second agent that reports its own window limit, and revisiting this was
considered on that basis. Its log records `contextWindow`, `thresholdTokens` and
`resolvedContextTokens` on every turn, which answers the lookup-table objection
completely: the number is the agent's own, not a table of ours to rot. It was
declined on three grounds. The source is `level: "debug"` output in a per-PID
log file, which nobody has promised to keep stable, unlike codex's
`model_context_window` in the session record. Only codex and omp expose a window
at all, so a percentage would appear on those rows and an absolute count on
every other, and inconsistency between adjacent rows is worse than either
choice applied uniformly. And this ADR governs the display contract rather than
data availability, so more data does not overturn it. That window figure is
used instead as a verification oracle, checking a provider's arithmetic against
the agent's own accounting. The condition for revisiting is a majority of
providers reporting a window limit through an interface that forms part of their
contract.
