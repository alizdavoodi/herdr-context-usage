# Ship no command-line interface

The plugin exists partly so coding agents can ask how full they or their
neighbours are, which sounds like it needs a queryable command. It does not.
herdr returns plugin-written metadata tokens from its own read commands, so
publishing the number into herdr also publishes it to every caller:

```bash
herdr pane current --current | jq '.result.pane.tokens'
herdr agent list | jq '.result.agents[] | {pane_id, agent, tokens}'
```

The plugin writes; herdr answers.

## Consequences

Nothing goes on `$PATH`. This matches the ecosystem — of eleven plugins
surveyed, nine are invoked only through herdr. Herdr starts commands in the
plugin root and exports `$HERDR_PLUGIN_ROOT` to the child process, but manifest
command arrays are executed literally and do not expand environment variables.
The manifest therefore invokes its binary as `./herdr-context-usage`.

Queries work even when the plugin is disabled or absent, because they read
herdr's state rather than ours. They are also stale by definition: a token is
only as fresh as the last event that wrote it. Given occupancy changes only at
turn boundaries, and events fire at turn boundaries, the staleness window is
normally empty.

A plugin cannot add a `herdr` subcommand, and `herdr plugin action invoke`
returns the started-command log record rather than the command's stdout, so
actions are unusable as a query interface regardless.

Discoverability moves to documentation: the plugin ships a `SKILL.md` naming the
two commands and the token names. Nothing else in this ecosystem does, and
herdr's own agent skill never mentions metadata tokens, so an agent has no way
to guess the capability exists.
