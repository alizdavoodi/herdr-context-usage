package pi

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
// state. The records are the faithful A-transcript copies in reader_test.go,
// replayed in their real order unless a case says otherwise.
func TestMeasure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setUp func(t *testing.T, home string) *provider.AgentSession
		// wantErr is the sentinel the failure must carry, nil for a measurement.
		wantErr           error
		wantCleared       bool
		wantJustCompacted bool
		want              int64
		wantMode          provider.ResolutionMode
		wantVersion       *int
	}{
		{
			// The occupancy is line 39's input + cacheRead + cacheWrite: 769 +
			// 27,136 + 0. Its totalTokens of 30,458 adds the turn's output, its
			// reasoning of 1,394 is a subset of that output, and 29,299 is the
			// reasoning wrongly added; the exact figure rules all three out.
			name: "a reported path measures the newest completed turn",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, sessionV3, turnA37, toolResultA38, turnA39)}
			},
			want:        27_905,
			wantMode:    provider.ResolutionAgentSession,
			wantVersion: intPtr(3),
		},
		{
			// The transcript sits under a directory naming a path the agent no
			// longer runs in, so only a match on the file name can find it.
			name: "a reported id is matched to its transcript whatever the pane's directory",
			setUp: func(t *testing.T, home string) *provider.AgentSession {
				path := writeStore(t, home, map[string][]string{homeSessionDir: {repoTranscript}})[repoTranscript]
				if err := os.WriteFile(path, []byte(strings.Join([]string{sessionV3, turnA39}, "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return &provider.AgentSession{Kind: "id", Value: repoSessionID}
			},
			want:        27_905,
			wantMode:    provider.ResolutionAgentSession,
			wantVersion: intPtr(3),
		},
		{
			name: "an id no transcript carries clears the row",
			setUp: func(*testing.T, string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "id", Value: repoSessionID}
			},
			wantErr:     ErrNoSession,
			wantCleared: true,
		},
		{
			// Two transcripts ending in the same id is exactly where a recency
			// tiebreak would look reasonable and be wrong.
			name: "an id two transcripts carry clears the row rather than picking one",
			setUp: func(t *testing.T, home string) *provider.AgentSession {
				writeStore(t, home, map[string][]string{
					repoSessionDir: {repoTranscript},
					homeSessionDir: {"2026-08-16T20-45-15-107Z_" + repoSessionID + ".jsonl"},
				})
				return &provider.AgentSession{Kind: "id", Value: repoSessionID}
			},
			wantErr:     ErrNoSession,
			wantCleared: true,
		},
		{
			// The case the documented integration precondition produces. No stage
			// resolved anything, so the mode stays empty for app to record as null;
			// a named mode here would claim a stage that never ran.
			name:        "no reported agent session clears the row with no mode",
			setUp:       func(*testing.T, string) *provider.AgentSession { return nil },
			wantErr:     ErrNoSession,
			wantCleared: true,
		},
		{
			// The trap this provider exists to avoid. pi's compaction record
			// carries its own usage, 1,383, and walking one record further would
			// report line 39's 27,905 for context that had just been thrown away;
			// the zero occupancy rules out both. Resolution succeeded and the
			// transcript was read up to the compaction record, so the mode and the
			// version still belong on the failed reading.
			name: "a compaction boundary clears the row as just-compacted",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, sessionV3, turnA39, compactionA40)}
			},
			wantErr:           ErrJustCompacted,
			wantCleared:       true,
			wantJustCompacted: true,
			wantMode:          provider.ResolutionAgentSession,
			wantVersion:       intPtr(3),
		},
		{
			// Line 42 was driven into the agent session to close the one link that
			// had been inferred rather than measured: the breakdown is not zeroed,
			// so ordinary arithmetic recovers. 17,051 is its own totalTokens, an
			// overstatement by the turn's output that would go unnoticed here.
			name: "the turn after a compaction is measured",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, sessionV3, turnA39, compactionA40, userA41, turnA42)}
			},
			want:        17_033,
			wantMode:    provider.ResolutionAgentSession,
			wantVersion: intPtr(3),
		},
		{
			// Blank, never zero, which would read as an empty window. The version
			// is what tells a maintainer the transcript was readable, so an
			// unanswered agent session is not mistaken for an unreadable file.
			name: "a session with no completed turn clears the row and keeps the version",
			setUp: func(t *testing.T, _ string) *provider.AgentSession {
				return &provider.AgentSession{Kind: "path", Value: writeTranscript(t, sessionV3, userA41)}
			},
			wantErr:     ErrNoOccupancy,
			wantCleared: true,
			wantMode:    provider.ResolutionAgentSession,
			wantVersion: intPtr(3),
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

// An agent home that cannot be resolved must clear the row. Left undeclared,
// the pane would keep a number that was correct once and stays plausible for as
// long as the pane lives: measured against pi 0.84.2 and herdr 0.8.0 with the
// answer left out of Superseded, the pane kept 27,905 from its previous round.
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
