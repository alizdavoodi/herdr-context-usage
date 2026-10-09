package claude

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
)

// What the round acts on is the pair Measure hands back and the verdict
// Superseded gives it, so each case pins the reading, the error and whether
// the value already on the pane is cleared or kept. The transcripts are the
// reader tests' fixtures, whose provenance is described there.
func TestMeasure(t *testing.T) {
	t.Parallel()

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
		wantSuperseded bool
		wantCompacted  bool
	}{
		{
			// The reported path is the first tier and process ids only a fallback
			// one, so a round in which herdr could not read the process list must
			// still measure the pane rather than drop it.
			name: "measures a reported path without a process list",
			pane: provider.Pane{AgentSession: reported("ordinary.jsonl")},
			want: provider.Reading{Mode: provider.ResolutionAgentSession, Occupancy: 149_479},
		},
		{
			// A claude process with no registry entry and no transcript under the
			// pane's directory: nothing names this pane's session, so whatever is
			// displayed belongs to one it has left.
			name: "clears a pane whose session cannot be resolved",
			pane: provider.Pane{
				ForegroundCWD: "/ambiguous",
				Processes:     []provider.Process{{PID: 42, Argv0: "claude"}},
			},
			wantErr:        ErrNoSession,
			wantRoot:       provider.ErrNoSession,
			wantSuperseded: true,
		},
		{
			// Resolution succeeded before the read failed, so its mode is still
			// recorded.
			name:           "clears a just-compacted transcript and keeps its mode",
			pane:           provider.Pane{AgentSession: reported("just-compacted.jsonl")},
			want:           provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:        ErrJustCompacted,
			wantRoot:       provider.ErrJustCompacted,
			wantSuperseded: true,
			wantCompacted:  true,
		},
		{
			// A turn that failed before it consumed anything writes all-zero usage;
			// reporting it would show an empty window, so it is unknown instead.
			name:           "clears a transcript whose only usage is a synthetic zero",
			pane:           provider.Pane{AgentSession: reported("zero-only.jsonl")},
			want:           provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:        ErrNoOccupancy,
			wantRoot:       provider.ErrNoOccupancy,
			wantSuperseded: true,
		},
		{
			name:           "clears a reported transcript that does not exist yet",
			pane:           provider.Pane{AgentSession: &provider.AgentSession{Kind: "path", Value: filepath.Join(home, "not-created-yet.jsonl")}},
			want:           provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:        fs.ErrNotExist,
			wantSuperseded: true,
		},
		{
			// A directory opens but cannot be read, which stands in for any I/O
			// failure: it says nothing about the session, so the value stays.
			name:    "keeps the value on a transcript I/O failure",
			pane:    provider.Pane{AgentSession: &provider.AgentSession{Kind: "path", Value: home}},
			want:    provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr: syscall.EISDIR,
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
			if compacted := errors.Is(err, provider.ErrJustCompacted); compacted != tt.wantCompacted {
				t.Fatalf("errors.Is(%v, ErrJustCompacted) = %t, want %t", err, compacted, tt.wantCompacted)
			}
		})
	}
}

func TestProcessIDsUsesExactBasenames(t *testing.T) {
	t.Parallel()

	processes := []provider.Process{
		{PID: 1, Argv0: "claude-mcp"},
		{PID: 2, Argv0: "/bin/claude"},
		{PID: 3, Argv0: "codex"},
	}
	if got := processIDs(processes); !slices.Equal(got, []int{2}) {
		t.Fatalf("processIDs() = %v, want [2]", got)
	}
}
