package provider

import (
	"context"
	"errors"
	"slices"
)

// ResolutionMode names which method identified the agent session for a pane
// (GLOSSARY.md). One set of names for every provider, so a measurement record
// reads the same whichever package produced it.
type ResolutionMode string

const (
	ResolutionAgentSession    ResolutionMode = "agent_session"
	ResolutionProcessRegistry ResolutionMode = "process_registry"
	ResolutionForegroundCWD   ResolutionMode = "foreground_cwd"
	ResolutionTerminalTitle   ResolutionMode = "terminal_title"
)

// Pane is what herdr reports about one pane that a provider may read. app
// maps herdr's agent record to it once; providers never see herdr's types,
// because herdr imports this package and the reverse import would be a cycle.
type Pane struct {
	ForegroundCWD string
	TerminalTitle string
	AgentSession  *AgentSession
	// Processes is the pane's foreground process list, empty when herdr could
	// not read it this round, and a provider using it as a resolution tier
	// then degrades to its other tiers. A failed read and an observed empty
	// list look the same here on purpose: only choosing between the opencode
	// versions has to tell them apart, and app does that before any provider
	// is picked.
	Processes []Process
}

// Reading is what one measurement observed. TranscriptVersion is the schema
// version the store stamped on the session it was read from, nil for a provider
// whose store carries none. A reader may populate it alongside a non-nil error —
// a just-compacted transcript was read successfully right up to the compaction
// record — so the refresh skeleton in app records it on the failure path too.
type Reading struct {
	// Mode is the zero value until a resolution stage succeeds. app is the one
	// place that turns that into the null the measurement record must carry, so
	// no provider allocates a pointer to say it resolved nothing. A failed
	// measurement still carries the mode of the stage that succeeded before it
	// failed.
	Mode              ResolutionMode
	Occupancy         int64
	ModelID           string
	TranscriptVersion *int
	// OracleDisagreement is a reader's own cross-check against a figure its
	// store computes independently, empty when the two agree or when the store
	// carries no such figure. It rides on a successful reading only: a read that
	// produced no occupancy had nothing to cross-check.
	OracleDisagreement string
}

// ErrJustCompacted is the state every provider reports in the interval after a
// compaction. Each provider wraps it, so the text names the agent while app
// recognises the state with one errors.Is.
var ErrJustCompacted = errors.New("agent session is just-compacted")

// Provider contributes the parts of a refresh that are specific to one agent
// kind. The skeleton in app owns error classification, measurement and
// reporting, which are identical across every provider.
type Provider interface {
	// Superseded lists this provider's answers that IsSuperseded treats as
	// proof the displayed value is wrong.
	Superseded() []error
	// Measure resolves the pane's agent session and reads its occupancy in one
	// call. A store-backed provider closes its own store before returning. An
	// unknown reading is an error, so that every successful numeric reading,
	// including zero, stays valid.
	Measure(ctx context.Context, pane Pane) (Reading, error)
}

// IsSuperseded decides what a failed measurement proves about the value already
// displayed for a pane. A declared answer proves it wrong, so the pane is
// cleared. Anything undeclared — a permission or I/O failure, a turn that
// recorded nothing — says nothing about the session and preserves it, so a new
// error defaults to keeping a value rather than blanking a pane.
func IsSuperseded(err error, declared []error) bool {
	return slices.ContainsFunc(declared, func(state error) bool { return errors.Is(err, state) })
}
