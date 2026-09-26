package app

import (
	"context"

	"github.com/alizdavoodi/herdr-context-usage/internal/herdr"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/omp"
	"github.com/alizdavoodi/herdr-context-usage/internal/report"
)

type ompProvider struct{}

func (ompProvider) Kind() provider.Kind { return provider.Omp }

func (ompProvider) States() report.States { return omp.SemanticStates() }

func (ompProvider) JustCompacted() error { return omp.ErrJustCompacted }

// Resolve builds the omp session input from OmpHome and herdr's reported agent
// session, and nothing else. Deliberately absent, as with pi: ForegroundCWD and
// the pane's process list. omp files an agent session under a directory derived
// from the one it started in, so a pane that has since moved would resolve to an
// unrelated transcript or to none, and the transcript does not mark a sub-agent
// session, so a directory-scoped match has nothing to exclude one with. Wiring
// either stage in by analogy with claude would trade a blank row for a wrong
// number. Reading is file-backed, so Cleanup is nil.
func (ompProvider) Resolve(_ context.Context, agent herdr.Agent, _ []provider.Process, config Config) (Resolved, error) {
	input := omp.SessionInput{OmpHome: config.OmpHome, AgentSession: agent.AgentSession}
	resolved, err := omp.Resolve(input)
	if err != nil {
		return Resolved{}, err
	}
	mode := string(resolved.Mode)
	path := resolved.Path
	return Resolved{
		Mode: &mode,
		Read: func(_ context.Context) (Reading, error) {
			// The version comes back even when the error does: a just-compacted
			// transcript was read right up to the compaction record, and that
			// row still has to name the version it was read from. The oracle
			// disagreement rides along on a successful reading, where it records
			// that omp's own prompt size stopped matching the sum without
			// withholding the sum.
			reading, err := omp.ReadOccupancy(path)
			return Reading{
				Occupancy:          reading.Occupancy,
				TranscriptVersion:  reading.Version,
				OracleDisagreement: reading.OracleDisagreement,
			}, err
		},
	}, nil
}
