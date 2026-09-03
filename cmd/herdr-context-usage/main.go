package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alizdavoodi/herdr-context-usage/internal/app"
	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
)

const refreshTimeout = 20 * time.Second

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	client := herdr.NewClient(os.Getenv("HERDR_BIN_PATH"), nil)
	config := app.Config{
		ClaudeHome: os.Getenv("CLAUDE_CONFIG_DIR"),
		CodexHome:  os.Getenv("CODEX_HOME"),
		OpenCodeDB: os.Getenv("OPENCODE_DB"),
		// The two opencode databases are overridable independently because a
		// machine can host both versions at once, each with its own store.
		OpenCode2DB: os.Getenv("OPENCODE2_DB"),
		// pi and omp share this one variable, so a user who exported it
		// redirected both agents at once and the plugin has to look where they
		// actually wrote rather than at either agent's default home. The two
		// fields stay separate all the same: unset, each provider falls back to
		// its own default home, and one field could not express that.
		PiHome:  os.Getenv("PI_CODING_AGENT_DIR"),
		OmpHome: os.Getenv("PI_CODING_AGENT_DIR"),
	}
	if err := app.Refresh(ctx, client, os.Getenv("HERDR_PANE_ID"), os.Stdout, config); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-context-usage: %v\n", err)
		os.Exit(1)
	}
}
