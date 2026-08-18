package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	occupancyformat "github.com/alizdavoodi/herdr-context-usage/internal/format"
	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

// Resolved carries one provider's session resolution: a closure that reads
// occupancy from the resolved handle, the resolution mode (nil until a
// resolution stage succeeds), and an optional cleanup for any store the
// resolution opened. A file-backed provider leaves Cleanup nil; a
// store-backed provider sets it to store.Close so the skeleton can defer it.
type Resolved struct {
	Read    func(context.Context) (int64, string, error)
	Mode    *string
	Cleanup func() error
}

// Provider contributes the parts of a refresh that are specific to one agent
// kind. The skeleton in refreshOne owns the error handling, measurement, and
// reporting that every provider shares.
type Provider interface {
	Kind() provider.Kind
	States() report.States
	JustCompacted() error
	Resolve(ctx context.Context, agent herdr.Agent, processes []provider.Process, config Config) (Resolved, error)
}

var allProviders = []Provider{
	claudeProvider{},
	codexProvider{},
	opencodeProvider{},
	opencode2Provider{},
}

func providerFor(kind provider.Kind) Provider {
	for _, p := range allProviders {
		if p.Kind() == kind {
			return p
		}
	}
	return nil
}

// refreshOne is the shared skeleton: resolve, read, and on either failure let
// handleUnknown decide whether to clear the pane. Each provider brings only
// its own resolution and reading; the error classification, measurement, and
// reporting are identical across all four.
func refreshOne(ctx context.Context, client Herdr, agent herdr.Agent, processes []provider.Process, log io.Writer, config Config, p Provider) error {
	resolved, err := p.Resolve(ctx, agent, processes, config)
	if resolved.Cleanup != nil {
		// A provider may open a store before its resolution fails; closing it
		// here is the original defer-store.Close path, even on the error branch.
		defer func() { _ = resolved.Cleanup() }()
	}
	if err != nil {
		return handleUnknown(ctx, client, agent.PaneID, log, p.Kind(), p.States(), ContextUnknown, err, resolved.Mode)
	}
	if resolved.Read == nil {
		// Refresh walks every pane, so a provider that resolves without
		// supplying a reader would panic the whole sweep rather than spoil one
		// row. Reporting it as unknown keeps the other panes measurable.
		err := fmt.Errorf("provider %s resolved without a reader", p.Kind())
		return handleUnknown(ctx, client, agent.PaneID, log, p.Kind(), p.States(), ContextUnknown, err, resolved.Mode)
	}
	value, modelID, err := resolved.Read(ctx)
	if err != nil {
		state := contextState(err, p.JustCompacted())
		return handleUnknown(ctx, client, agent.PaneID, log, p.Kind(), p.States(), state, err, resolved.Mode)
	}
	measurement := Measurement{
		PaneID: agent.PaneID, Provider: p.Kind(), ContextState: ContextMeasured,
		Occupancy: &value, ResolutionMode: resolved.Mode, ModelID: modelID,
	}
	reportErr := client.Report(ctx, agent.PaneID, occupancyformat.Occupancy(value), strconv.FormatInt(value, 10))
	measurementErr := writeMeasurement(log, measurement)
	return errors.Join(measurementErr, reportErr)
}
