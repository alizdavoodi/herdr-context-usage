package omp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// repoCWD is set on every pane and is deliberately useless: this provider has
// no working-directory stage, so a case that resolves must resolve from the
// reported agent session alone.
const repoCWD = "/Users/alirezadavoodi/projects/github.com/alizdavoodi/herdr-context-usage"

// What the round acts on, read through Measure: the reading, whether the
// failure clears the row or keeps it, and whether it is the just-compacted
// state. The records are the faithful C-transcript copies in reader_test.go,
// replayed in their real order unless a case says otherwise.
func TestMeasure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setUp func(t *testing.T, home string) *provider.AgentSession
		// wantErr is the sentinel the failure must carry, nil for a measurement.
		wantErr           error
		wantRoot          error
		wantCleared       bool
		wantJustCompacted bool
		want              int64
		wantMode          provider.ResolutionMode
		wantVersion       *int
		// The numbers the oracle diagnostic must name, or none when it must stay
		// silent: agreement that spoke would appear on every omp row and mean
		// nothing.
		wantOracleNaming []string
	}{
		{
			// The occupancy is line 250's input + cacheRead + cacheWrite: 132,098 +
			// 0 + 0. The exact figure rules out its totalTokens of 134,693, which
			// is also the compaction record's tokensBefore, and its
			// nonMessageTokens of 19,016 whether read or added. It also keeps the
			// window limit out: ADR-0001 declined to display omp's known
			// 1,000,000 window, so the reading is the count and nothing relative.
			name: "a reported path measures the newest completed turn",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, titleC1, sessionC2, turnC247, toolResultC246, turnC250)}
			},
			want:        132_098,
			wantMode:    provider.ResolutionAgentSession,
			wantVersion: intPtr(3),
		},
		{
			// The transcript sits under a directory naming a path the agent no
			// longer runs in, so only a match on the file name can find it.
			name: "a reported id is matched to its transcript whatever the pane's directory",
			setUp: func(t *testing.T, home string) *provider.AgentSession {
				path := writeStore(t, home, map[string][]string{otherSessionDir: {repoTranscript}})[repoTranscript]
				if err := os.WriteFile(path, []byte(strings.Join([]string{titleC1, sessionC2, turnC250}, "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &provider.AgentSession{Kind: "id", Value: repoSessionID}
			},
			want:        132_098,
			wantMode:    provider.ResolutionAgentSession,
			wantVersion: intPtr(3),
		},
		{
			name: "an id no transcript carries clears the row",
			setUp: func(*testing.T, string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "id", Value: repoSessionID}
			},
			wantErr:     ErrNoSession,
			wantRoot:    provider.ErrNoSession,
			wantCleared: true,
		},
		{
			// Two transcripts ending in the same id is exactly where a recency
			// tiebreak would look reasonable and be wrong.
			name: "an id two transcripts carry clears the row rather than picking one",
			setUp: func(t *testing.T, home string) *provider.AgentSession {
				writeStore(t, home, map[string][]string{
					repoSessionDir:  {repoTranscript},
					otherSessionDir: {"2026-08-19T09-00-00-000Z_" + repoSessionID + ".jsonl"},
				})
				return &provider.AgentSession{Kind: "id", Value: repoSessionID}
			},
			wantErr:     ErrNoSession,
			wantRoot:    provider.ErrNoSession,
			wantCleared: true,
		},
		{
			// herdr labels an omp pane from its process name whether or not omp's
			// integration is installed, as herdr 0.8.0 does with omp 18.1.4, so
			// without the integration the pane arrives with nothing to resolve.
			name:        "no reported agent session clears the row with no mode",
			setUp:       func(*testing.T, string) *provider.AgentSession { return nil },
			wantErr:     ErrNoSession,
			wantRoot:    provider.ErrNoSession,
			wantCleared: true,
		},
		{
			// The trap this provider exists to avoid, failing the opposite way
			// from pi's: omp's compaction record carries no usage, so a walk for
			// the newest usage would sail past it and report line 250's 132,098, a
			// 3.2x overstatement of the 40,710 held one turn later. The zero
			// occupancy rules that out. Resolution succeeded and the transcript
			// was read up to the compaction record, so the mode and the version
			// still belong on the failed reading.
			name: "a compaction boundary clears the row as just-compacted",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, titleC1, sessionC2, turnC250, customC251, compactionC252)}
			},
			wantErr:           ErrJustCompacted,
			wantRoot:          provider.ErrJustCompacted,
			wantCleared:       true,
			wantJustCompacted: true,
			wantMode:          provider.ResolutionAgentSession,
			wantVersion:       intPtr(3),
		},
		{
			// The breakdown carries no marker of the compaction, so ordinary
			// arithmetic recovers. 40,748 is the turn's own totalTokens, an
			// overstatement by its output that would go unnoticed here.
			name: "the turn after a compaction is measured",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, titleC1, sessionC2, turnC250, customC251, compactionC252, userC253, turnC254)}
			},
			want:        40_710,
			wantMode:    provider.ResolutionAgentSession,
			wantVersion: intPtr(3),
		},
		{
			// Blank, never zero, which would read as an empty window. The version
			// is what tells a maintainer the transcript was readable, so an
			// unanswered agent session is not mistaken for an unreadable file.
			name: "a session with no completed turn clears the row and keeps the version",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, titleC1, sessionC2, userC253)}
			},
			wantErr:     ErrNoOccupancy,
			wantRoot:    provider.ErrNoOccupancy,
			wantCleared: true,
			wantMode:    provider.ResolutionAgentSession,
			wantVersion: intPtr(3),
		},
		{
			// The guard against promoting the oracle to the source: reading the
			// stand-in's 41,910 would pass every other case here. A disagreement is
			// a fact about omp's format, so it rides beside the sum and never
			// withholds it, and it names both numbers so a maintainer can see which
			// way omp moved.
			name: "an oracle disagreement is recorded and the sum still measured",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, titleC1, sessionC2, userC253, turnC254OracleDisagrees)}
			},
			want:             40_710,
			wantMode:         provider.ResolutionAgentSession,
			wantVersion:      intPtr(3),
			wantOracleNaming: []string{"41910", "40710"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			p := New(home)
			got, err := p.Measure(context.Background(), provider.Pane{ForegroundCWD: repoCWD, AgentSession: tt.setUp(t, home)})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Measure() error = %v, want %v", err, tt.wantErr)
			}
			if cleared := provider.IsSuperseded(err, p.Superseded()); cleared != tt.wantCleared {
				t.Fatalf("IsSuperseded() = %t, want %t", cleared, tt.wantCleared)
			}
			if tt.wantRoot != nil && !errors.Is(err, tt.wantRoot) {
				t.Fatalf("Measure() error = %v, want it to wrap the shared root %v", err, tt.wantRoot)
			}
			if justCompacted := errors.Is(err, provider.ErrJustCompacted); justCompacted != tt.wantJustCompacted {
				t.Fatalf("errors.Is(err, provider.ErrJustCompacted) = %t, want %t", justCompacted, tt.wantJustCompacted)
			}
			if got.Occupancy != tt.want {
				t.Fatalf("Measure() occupancy = %d, want %d", got.Occupancy, tt.want)
			}
			if got.Mode != tt.wantMode {
				t.Fatalf("Measure() mode = %q, want %q", got.Mode, tt.wantMode)
			}
			if !sameVersion(got.TranscriptVersion, tt.wantVersion) {
				t.Fatalf("Measure() version = %s, want %s", formatVersion(got.TranscriptVersion), formatVersion(tt.wantVersion))
			}
			if len(tt.wantOracleNaming) == 0 && got.OracleDisagreement != "" {
				t.Fatalf("Measure() oracle disagreement = %q, want none", got.OracleDisagreement)
			}
			for _, number := range tt.wantOracleNaming {
				if !strings.Contains(got.OracleDisagreement, number) {
					t.Fatalf("Measure() oracle disagreement = %q, want it to name %s", got.OracleDisagreement, number)
				}
			}
		})
	}
}

// A read that fails for a reason the provider never claimed says nothing about
// the agent session, so the displayed value stands. A reported path that is a
// directory opens and then fails to read, the same class of answer as the
// permission error this guards against.
func TestMeasureKeepsTheRowOnATransientReadFailure(t *testing.T) {
	t.Parallel()

	p := New(t.TempDir())
	_, err := p.Measure(context.Background(), provider.Pane{AgentSession: &provider.AgentSession{Kind: "path", Value: t.TempDir()}})
	if err == nil {
		t.Fatal("Measure() error = nil, want the unreadable transcript reported")
	}
	if provider.IsSuperseded(err, p.Superseded()) {
		t.Fatalf("Measure() error = %v is superseded, want a transient failure to keep the displayed value", err)
	}
}

// An agent home that cannot be resolved must clear the row. Left unclassified,
// the pane would keep a number that was correct once and stays plausible for as
// long as the pane lives: measured against omp 17.3.5 and herdr 0.8.0 with the
// answer left out of the classification, the pane kept 132,098 from its
// previous round.
//
// It must also stay distinguishable from an unresolved agent session, because
// the fixes differ: export a home, against install the integration.
//
// Sets the environment, so it cannot run in parallel. Unsetting HOME defeats
// os.UserHomeDir only on unix; Windows reads USERPROFILE. Only the id branch
// reaches a home; a reported path without one is TestResolveAPathWithoutAResolvableHome.
func TestMeasureClearsTheRowWhenTheAgentHomeCannotBeResolved(t *testing.T) {
	t.Setenv("HOME", "")

	p := New("")
	got, err := p.Measure(context.Background(), provider.Pane{AgentSession: &provider.AgentSession{Kind: "id", Value: repoSessionID}})
	if !errors.Is(err, provider.ErrNoHome) || errors.Is(err, ErrNoSession) {
		t.Fatalf("Measure() error = %v, want %v and not %v", err, provider.ErrNoHome, ErrNoSession)
	}
	if !provider.IsSuperseded(err, p.Superseded()) {
		t.Fatalf("Measure() error = %v is not superseded, want an unresolvable home to clear the row", err)
	}
	if got.Occupancy != 0 || got.Mode != "" {
		t.Fatalf("Measure() = %+v, want no occupancy and no mode", got)
	}
}
