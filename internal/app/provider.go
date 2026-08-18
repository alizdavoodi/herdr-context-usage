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

// Reading is what a provider's reader observed. TranscriptVersion is the schema
// version the store stamped on the session it was read from, nil for a provider
// whose store carries none. A reader may populate it alongside a non-nil error —
// a just-compacted transcript was read successfully right up to the compaction
// record — so refreshOne records it on the failure path too.
type Reading struct {
	Occupancy         int64
	ModelID           string
	TranscriptVersion *int
}

// Resolved carries one provider's session resolution: a closure that reads a
// Reading from the resolved handle, the resolution mode (nil until a
// resolution stage succeeds), and an optional cleanup for any store the
// resolution opened. A file-backed provider leaves Cleanup nil; a
// store-backed provider sets it to store.Close so the skeleton can defer it.
type Resolved struct {
	Read    func(context.Context) (Reading, error)
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

// Adding a provider here is what makes its kind measurable, and it is not
// enough on its own: dispatch reaches this list only through provider.FromAgent,
// so a kind that function never returns stays unreachable however complete its
// package is. internal/app/seam_test.go pins both halves.
var allProviders = []Provider{
	claudeProvider{},
	codexProvider{},
	opencodeProvider{},
	opencode2Provider{},
	piProvider{},
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
// reporting are identical across every provider.
func refreshOne(ctx context.Context, client Herdr, agent herdr.Agent, processes []provider.Process, log io.Writer, config Config, p Provider) error {
	resolved, err := p.Resolve(ctx, agent, processes, config)
	if resolved.Cleanup != nil {
		// A provider may open a store before its resolution fails; closing it
		// here is the original defer-store.Close path, even on the error branch.
		defer func() { _ = resolved.Cleanup() }()
	}
	// Every outcome below records the same shape and differs only in the state
	// and in how much the reader got to observe first, so the record is built
	// once here and handed on rather than passed down field by field.
	measurement := Measurement{
		PaneID: agent.PaneID, Provider: p.Kind(), ContextState: ContextUnknown,
		ResolutionMode: resolved.Mode,
	}
	if err != nil {
		return handleUnknown(ctx, client, log, p.States(), measurement, err)
	}
	if resolved.Read == nil {
		// Refresh walks every pane, so a provider that resolves without
		// supplying a reader would panic the whole sweep rather than spoil one
		// row. Reporting it as unknown keeps the other panes measurable.
		err := fmt.Errorf("provider %s resolved without a reader", p.Kind())
		return handleUnknown(ctx, client, log, p.States(), measurement, err)
	}
	reading, err := resolved.Read(ctx)
	// A failed read can still have observed the schema version, and that is the
	// case where knowing it matters most: a blank row caused by a format change
	// is otherwise indistinguishable from any other unreadable transcript.
	measurement.TranscriptVersion = reading.TranscriptVersion
	if err != nil {
		measurement.ContextState = contextState(err, p.JustCompacted())
		return handleUnknown(ctx, client, log, p.States(), measurement, err)
	}
	measurement.ContextState = ContextMeasured
	measurement.Occupancy = &reading.Occupancy
	measurement.ModelID = reading.ModelID
	reportErr := client.Report(ctx, agent.PaneID, occupancyformat.Occupancy(reading.Occupancy), strconv.FormatInt(reading.Occupancy, 10))
	measurementErr := writeMeasurement(log, measurement)
	return errors.Join(measurementErr, reportErr)
}
