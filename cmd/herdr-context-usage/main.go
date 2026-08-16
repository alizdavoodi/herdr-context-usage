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
	if err := app.Refresh(ctx, client, os.Getenv("HERDR_PANE_ID"), os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-context-usage: %v\n", err)
		os.Exit(1)
	}
}
