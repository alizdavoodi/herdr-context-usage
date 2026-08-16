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

This correction was checked against Herdr 0.8.0's built-in row tokens:
`state_icon`, `state_text`, `workspace`, `tab`, `pane`, `agent`,
`terminal_title`, and `terminal_title_stripped`. None is a model token; a
`$name` slot comes from pane metadata written by a plugin. Do not re-add the
previous model-token claim from memory.

herdr's sidebar styling is static per slot and cannot vary by value, so a
severity colour was never available anyway. Adding a percentage later means
adding the table and its maintenance, not rewriting the extractors.
