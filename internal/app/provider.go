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
// enough on its own: dispatch reaches this map only through provider.FromAgent,
// so a kind that function never returns stays unreachable however complete its
// package is. internal/app/seam_test.go pins both halves. Two providers cannot
// claim one kind, because Go rejects a duplicate constant key in a map literal
// and so the registry would not compile.
//
// Each provider is handed only the config it reads, once; Config.PiCodingAgentDir
// says why pi and omp share a field.
func registry(config Config) map[provider.Kind]provider.Provider {
	return map[provider.Kind]provider.Provider{
		provider.Claude:    claude.New(config.ClaudeHome),
		provider.Codex:     codex.New(config.CodexHome),
		provider.OpenCode:  opencode.New(config.OpenCodeDB, config.OpenCodeDataHome),
		provider.OpenCode2: opencode2.New(config.OpenCode2DB, config.OpenCodeDataHome),
		provider.Pi:        pi.New(config.PiCodingAgentDir),
		provider.Omp:       omp.New(config.PiCodingAgentDir),
		provider.Hermes:    hermes.New(config.HermesHome),
	}
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

// refreshOne is the shared skeleton: measure, then report the reading or decide
// whether a failed one clears the pane. Each provider brings only its own
// resolution and reading; the error classification, measurement, and reporting
// are identical across every provider.
func refreshOne(ctx context.Context, client Herdr, paneID string, kind provider.Kind, pane provider.Pane, log io.Writer, p provider.Provider, seq int64) error {
	reading, err := p.Measure(ctx, pane)
	// Every outcome below records the same shape and differs only in the state
	// and in how much the provider got to observe first, so the record is built
	// once here and handed on rather than passed down field by field.
	measurement := Measurement{
		PaneID: paneID, Provider: kind, ContextState: ContextUnknown,
		ResolutionMode: resolutionMode(reading.Mode),
		// A failed read can still have observed the schema version, and that is
		// the case where knowing it matters most: a blank row caused by a format
		// change is otherwise indistinguishable from any other unreadable
		// transcript.
		TranscriptVersion: reading.TranscriptVersion,
	}
	if err != nil {
		if errors.Is(err, provider.ErrJustCompacted) {
			measurement.ContextState = ContextJustCompacted
		}
		measurement.Error = err.Error()
		measurementErr := writeMeasurement(log, measurement)
		if provider.IsSuperseded(err, p.Superseded()) {
			return errors.Join(measurementErr, client.Clear(ctx, paneID, seq))
		}
		// Anything undeclared leaves the displayed value alone. An answer such as
		// codex's ErrFailedTurn leaves the previous reading standing as a valid
		// lower bound, a transient one is a failure to read, and neither is
		// evidence that the value belongs to a superseded session.
		return measurementErr
	}
	measurement.ContextState = ContextMeasured
	measurement.Occupancy = &reading.Occupancy
	measurement.ModelID = reading.ModelID
	measurement.OracleDisagreement = reading.OracleDisagreement
	reportErr := client.Report(ctx, paneID, formatOccupancy(reading.Occupancy), strconv.FormatInt(reading.Occupancy, 10), seq)
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
