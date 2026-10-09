package codex

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// What the round acts on is the pair Measure hands back and the verdict
// Superseded gives it, so each case pins the reading, the error and whether
// the value already on the pane is cleared or kept. The rollouts are the
// reader tests' fixtures, whose provenance is described there.
func TestMeasure(t *testing.T) {
	t.Parallel()

	// An empty home rather than the default, so no case depends on what the
	// host's own ~/.codex holds.
	home := t.TempDir()
	reported := func(name string) *provider.AgentSession {
		path, err := filepath.Abs(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return &provider.AgentSession{Kind: "path", Value: path}
	}

	tests := []struct {
		name           string
		pane           provider.Pane
		want           provider.Reading
		wantErr        error
		wantRoot       error
		wantNotRoot    error
		wantSuperseded bool
		wantCompacted  bool
	}{
		{
			// The reading after the compaction is the absolute 18,162, not the
			// 245,715 that preceded it.
			name: "reports the post-compaction occupancy",
			pane: provider.Pane{AgentSession: reported("compacted.jsonl")},
			want: provider.Reading{Mode: provider.ResolutionAgentSession, Occupancy: 18_162},
		},
		{
			// Codex reads only the newest usage row, so a turn that recorded
			// nothing must leave the previous reading standing rather than blank
			// the pane: occupancy only rises within a turn. It is its own answer,
			// not the shared no-occupancy outcome, which is why it does not
			// supersede.
			name:        "keeps the value after a turn that recorded no usage",
			pane:        provider.Pane{AgentSession: reported("zero-total.jsonl")},
			want:        provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:     ErrFailedTurn,
			wantNotRoot: provider.ErrNoOccupancy,
		},
		{
			// A session that never took a turn has produced no usage at all, which
			// is the shared no-occupancy outcome and clears the pane.
			name:           "clears a rollout whose session took no turn",
			pane:           provider.Pane{AgentSession: reported("no-usage.jsonl")},
			want:           provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:        ErrNoOccupancy,
			wantRoot:       provider.ErrNoOccupancy,
			wantSuperseded: true,
		},
		{
			// Codex names a rollout before it writes it, so a missing file is a
			// session that is not this pane's yet, not an I/O failure.
			name:           "clears a reported rollout that is not written yet",
			pane:           provider.Pane{AgentSession: &provider.AgentSession{Kind: "path", Value: filepath.Join(home, "not-written.jsonl")}},
			want:           provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:        fs.ErrNotExist,
			wantSuperseded: true,
		},
		{
			// Resolution succeeded before the read failed, so its mode is still
			// recorded.
			name:           "clears a just-compacted rollout and keeps its mode",
			pane:           provider.Pane{AgentSession: reported("just-compacted.jsonl")},
			want:           provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:        ErrJustCompacted,
			wantRoot:       provider.ErrJustCompacted,
			wantSuperseded: true,
			wantCompacted:  true,
		},
		{
			// The pane an agent exited from: no reported session, and no live
			// rollout under the pane's directory. The value displayed belongs
			// to a session that is gone.
			name: "clears a pane with no reported session and no live rollout",
			pane: provider.Pane{
				ForegroundCWD: "/work",
			},
			wantErr:        ErrNoSession,
			wantRoot:       provider.ErrNoSession,
			wantSuperseded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := New(home)
			got, err := p.Measure(context.Background(), tt.pane)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Measure() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Measure() = %#v, want %#v", got, tt.want)
			}
			if superseded := provider.IsSuperseded(err, p.Superseded()); superseded != tt.wantSuperseded {
				t.Fatalf("IsSuperseded(%v) = %t, want %t", err, superseded, tt.wantSuperseded)
			}
			if tt.wantRoot != nil && !errors.Is(err, tt.wantRoot) {
				t.Fatalf("Measure() error = %v, want it to wrap the shared root %v", err, tt.wantRoot)
			}
			if tt.wantNotRoot != nil && errors.Is(err, tt.wantNotRoot) {
				t.Fatalf("Measure() error = %v, want it kept separate from the shared root %v", err, tt.wantNotRoot)
			}
			if compacted := errors.Is(err, provider.ErrJustCompacted); compacted != tt.wantCompacted {
				t.Fatalf("errors.Is(%v, ErrJustCompacted) = %t, want %t", err, compacted, tt.wantCompacted)
			}
		})
	}
}
