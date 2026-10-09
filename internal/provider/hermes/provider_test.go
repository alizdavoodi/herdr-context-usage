package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

// homeOf returns the agent home holding a state.db written by newStateDB.
func homeOf(t *testing.T, sessions ...fixtureSession) string {
	t.Helper()
	return filepath.Dir(newStateDB(t, sessions...))
}

func TestMeasureReportsTheUsageAnchorForAReportedID(t *testing.T) {
	t.Parallel()

	p := New(homeOf(t, fixtureSession{ID: openID, Model: "claude-fable-5-1", Config: anchor("29452")}))
	// ForegroundCWD is set and useless: this provider has no directory stage, so
	// the pane must resolve from the reported id alone.
	got, err := p.Measure(context.Background(), provider.Pane{ForegroundCWD: "/work", AgentSession: &provider.AgentSession{Kind: "id", Value: openID}})
	if err != nil {
		t.Fatalf("Measure() error = %v", err)
	}
	if got.Occupancy != 29452 || got.ModelID != "claude-fable-5-1" || got.Mode != provider.ResolutionAgentSession {
		t.Fatalf("Measure() = %+v, want 29452 on claude-fable-5-1 under agent_session", got)
	}
	if got.TranscriptVersion == nil || *got.TranscriptVersion != 30 {
		t.Fatalf("TranscriptVersion = %v, want 30", got.TranscriptVersion)
	}
}

func TestMeasureClearsOnlyWhatProvesTheDisplayedValueWrong(t *testing.T) {
	t.Parallel()

	reported := &provider.AgentSession{Kind: "id", Value: openID}
	tests := []struct {
		name    string
		session *provider.AgentSession
		home    func(*testing.T) string
		// wantErr nil accepts any error, for a failure no sentinel names.
		wantErr           error
		wantRoot          error
		wantMode          provider.ResolutionMode
		wantSuperseded    bool
		wantJustCompacted bool
	}{
		{
			name:           "no agent session reported",
			home:           func(t *testing.T) string { return homeOf(t, fixtureSession{ID: openID, Config: anchor("29452")}) },
			wantErr:        ErrNoSession,
			wantRoot:       provider.ErrNoSession,
			wantSuperseded: true,
		},
		{
			// A desktop pane keeps reporting a session it closed days ago, and its
			// anchor is still a real number for that closed session.
			name:    "reported session has ended",
			session: reported,
			home: func(t *testing.T) string {
				return homeOf(t, fixtureSession{ID: openID, Config: anchor("29452"), Ended: true, EndReason: "cli_close"})
			},
			wantErr:        ErrSessionEnded,
			wantMode:       provider.ResolutionAgentSession,
			wantSuperseded: true,
		},
		{
			name:              "compacted in place",
			session:           reported,
			home:              func(t *testing.T) string { return homeOf(t, fixtureSession{ID: openID, Config: `{}`, Compacted: true}) },
			wantErr:           ErrJustCompacted,
			wantRoot:          provider.ErrJustCompacted,
			wantMode:          provider.ResolutionAgentSession,
			wantSuperseded:    true,
			wantJustCompacted: true,
		},
		{
			// The anchor clear Hermes runs after compacting is a separate write that
			// can fail silently, leaving the pre-compaction figure in place.
			name:    "anchor outlives an in-place compaction",
			session: reported,
			home: func(t *testing.T) string {
				return homeOf(t, fixtureSession{ID: openID, Config: anchor("29452"), Active: 3, Compacted: true})
			},
			wantErr:           ErrJustCompacted,
			wantRoot:          provider.ErrJustCompacted,
			wantMode:          provider.ResolutionAgentSession,
			wantSuperseded:    true,
			wantJustCompacted: true,
		},
		{
			name:    "rotated by compaction",
			session: reported,
			home: func(t *testing.T) string {
				return homeOf(t, fixtureSession{ID: openID, Config: anchor("29452"), Ended: true, EndReason: "compression"})
			},
			wantErr:           ErrJustCompacted,
			wantRoot:          provider.ErrJustCompacted,
			wantMode:          provider.ResolutionAgentSession,
			wantSuperseded:    true,
			wantJustCompacted: true,
		},
		{
			// The session row exists but has not been priced yet: no anchor and no
			// compaction marker, which is the shared no-occupancy outcome.
			name:           "session has produced no reading",
			session:        reported,
			home:           func(t *testing.T) string { return homeOf(t, fixtureSession{ID: openID, Config: `{}`}) },
			wantErr:        ErrNoOccupancy,
			wantRoot:       provider.ErrNoOccupancy,
			wantMode:       provider.ResolutionAgentSession,
			wantSuperseded: true,
		},
		{
			name:           "store not written yet",
			session:        reported,
			home:           func(t *testing.T) string { return t.TempDir() },
			wantErr:        sqlitestore.ErrMissing,
			wantMode:       provider.ResolutionAgentSession,
			wantSuperseded: true,
		},
		{
			// A directory where the database should be is not evidence about the
			// agent session, so the displayed value must survive it.
			name:    "store unreadable",
			session: reported,
			home: func(t *testing.T) string {
				home := t.TempDir()
				if err := os.Mkdir(filepath.Join(home, "state.db"), 0o700); err != nil {
					t.Fatal(err)
				}
				return home
			},
			wantMode: provider.ResolutionAgentSession,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := New(tt.home(t))
			got, err := p.Measure(context.Background(), provider.Pane{ForegroundCWD: "/work", AgentSession: tt.session})
			if err == nil || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
				t.Fatalf("Measure() = %+v, %v; want error %v", got, err, tt.wantErr)
			}
			// The mode survives a failed read, so the record names the stage that
			// accepted the reported id.
			if got.Mode != tt.wantMode {
				t.Fatalf("Mode = %q, want %q", got.Mode, tt.wantMode)
			}
			if superseded := provider.IsSuperseded(err, p.Superseded()); superseded != tt.wantSuperseded {
				t.Fatalf("IsSuperseded(%v) = %v, want %v", err, superseded, tt.wantSuperseded)
			}
			if tt.wantRoot != nil && !errors.Is(err, tt.wantRoot) {
				t.Fatalf("Measure() error = %v, want it to wrap the shared root %v", err, tt.wantRoot)
			}
			if justCompacted := errors.Is(err, provider.ErrJustCompacted); justCompacted != tt.wantJustCompacted {
				t.Fatalf("errors.Is(%v, ErrJustCompacted) = %v, want %v", err, justCompacted, tt.wantJustCompacted)
			}
		})
	}
}
