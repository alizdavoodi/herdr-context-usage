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
a 200k window and dire on a 128k one. Mitigated at config time rather than in
the plugin: the model is available as a separate sidebar token, so a row can
read `92k · opus` without the plugin resolving any limit.

herdr's sidebar styling is static per slot and cannot vary by value, so a
severity colour was never available anyway. Adding a percentage later means
adding the table and its maintenance, not rewriting the extractors.
