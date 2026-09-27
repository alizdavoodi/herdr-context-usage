package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/claude"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/codex"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/hermes"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/omp"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencode"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencode2"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/pi"
)

// Adding a provider here is what makes its kind measurable, and it is not
// enough on its own: dispatch reaches this list only through provider.FromAgent,
// so a kind that function never returns stays unreachable however complete its
// package is. internal/app/seam_test.go pins both halves.
//
// Each provider is handed only the config it reads, once; Config.PiCodingAgentDir
// says why pi and omp share a field.
func registry(config Config) []provider.Provider {
	return []provider.Provider{
		claude.New(config.ClaudeHome),
		codex.New(config.CodexHome),
		opencode.New(config.OpenCodeDB, config.OpenCodeDataHome),
		opencode2.New(config.OpenCode2DB, config.OpenCodeDataHome),
		pi.New(config.PiCodingAgentDir),
		omp.New(config.PiCodingAgentDir),
		hermes.New(config.HermesHome),
	}
}

func providerFor(kind provider.Kind, registered []provider.Provider) provider.Provider {
	for _, p := range registered {
		if p.Kind() == kind {
			return p
		}
	}
	return nil
}

// paneOf is the one place herdr's agent record becomes what a provider reads,
// so a field herdr adds or renames is absorbed here rather than in every
// provider.
func paneOf(agent herdr.Agent, processes []provider.Process) provider.Pane {
	return provider.Pane{
		ForegroundCWD: agent.ForegroundCWD,
		TerminalTitle: agent.TerminalTitle,
		AgentSession:  agent.AgentSession,
		Processes:     processes,
	}
}

// refreshOne is the shared skeleton: resolve, read, and on either failure let
// handleUnknown decide whether to clear the pane. Each provider brings only
// its own resolution and reading; the error classification, measurement, and
// reporting are identical across every provider.
func refreshOne(ctx context.Context, client Herdr, paneID string, pane provider.Pane, log io.Writer, p provider.Provider) error {
	resolved, err := p.Resolve(ctx, pane)
	if resolved.Cleanup != nil {
		// A provider may open a store before its resolution fails; closing it
		// here is the original defer-store.Close path, even on the error branch.
		defer func() { _ = resolved.Cleanup() }()
	}
	// Every outcome below records the same shape and differs only in the state
	// and in how much the reader got to observe first, so the record is built
	// once here and handed on rather than passed down field by field.
	measurement := Measurement{
		PaneID: paneID, Provider: p.Kind(), ContextState: ContextUnknown,
		ResolutionMode: resolutionMode(resolved.Mode),
	}
	if err != nil {
		return handleUnknown(ctx, client, log, p.Superseded(), measurement, err)
	}
	if resolved.Read == nil {
		// Refresh walks every pane, so a provider that resolves without
		// supplying a reader would panic the whole sweep rather than spoil one
		// row. Reporting it as unknown keeps the other panes measurable.
		err := fmt.Errorf("provider %s resolved without a reader", p.Kind())
		return handleUnknown(ctx, client, log, p.Superseded(), measurement, err)
	}
	reading, err := resolved.Read(ctx)
	// A failed read can still have observed the schema version, and that is the
	// case where knowing it matters most: a blank row caused by a format change
	// is otherwise indistinguishable from any other unreadable transcript.
	measurement.TranscriptVersion = reading.TranscriptVersion
	if err != nil {
		measurement.ContextState = contextState(err, p.JustCompacted())
		return handleUnknown(ctx, client, log, p.Superseded(), measurement, err)
	}
	measurement.ContextState = ContextMeasured
	measurement.Occupancy = &reading.Occupancy
	measurement.ModelID = reading.ModelID
	measurement.OracleDisagreement = reading.OracleDisagreement
	reportErr := client.Report(ctx, paneID, formatOccupancy(reading.Occupancy), strconv.FormatInt(reading.Occupancy, 10))
	measurementErr := writeMeasurement(log, measurement)
	return errors.Join(measurementErr, reportErr)
}

// formatOccupancy keeps the sidebar compact while ctx_raw remains the exact machine value.
func formatOccupancy(tokens int64) string {
	switch {
	case tokens < 1_000:
		return fmt.Sprintf("%d", tokens)
	case tokens < 999_500:
		return fmt.Sprintf("%.0fk", float64(tokens)/1_000)
	default:
		millions := math.Round(float64(tokens)/100_000) / 10
		return fmt.Sprintf("%.1fM", millions)
	}
}

// resolutionMode is the one place an unresolved pane's mode becomes null. A
// provider leaves Mode at its zero value until a stage succeeds, and the record
// must say null there: an empty string would read as a mode that exists but has
// no name.
func resolutionMode(mode provider.ResolutionMode) *provider.ResolutionMode {
	if mode == "" {
		return nil
	}
	return &mode
}
