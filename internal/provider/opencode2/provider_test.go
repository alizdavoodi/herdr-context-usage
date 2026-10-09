package opencode2

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/sqlitestore"
)

type testMessage struct {
	id, sessionID, messageType string
	seq                        int64
	data                       string
}

// measured is a turn from the live store: the session title and directory are
// the live opencode2 pane this plugin was verified against, which herdr reported
// as `OC | Probe test response`, and 2,445 / 14,848 is the measured turn where
// input is smaller than cache.read — the observation that settled that
// opencode2 normalises cache out of input and that the three fields add.
const measured = `{"time":{"completed":1},"model":{"id":"deepseek-v4-pro"},"tokens":{"input":2445,"cache":{"read":14848,"write":5}}}`

// Each case is one pane measured the way a refresh round measures it, asserting
// on everything the round acts on: the reading it reports, whether a failure
// clears the displayed value or keeps it, and whether it is the just-compacted
// state. The ladder is the reported root id, then the title; mode is set only by
// the stage that succeeded.
func TestMeasureResolvesThroughTheLadderAndClassifiesWhatItFinds(t *testing.T) {
	t.Parallel()

	tui := []provider.Process{{PID: 42, Argv0: "opencode2"}}
	titled := func(title string) provider.Pane {
		return provider.Pane{ForegroundCWD: "/work/acme", TerminalTitle: title, Processes: tui}
	}
	reported := func(id string) provider.Pane {
		pane := titled("OC | Probe test response")
		pane.AgentSession = &provider.AgentSession{Kind: "id", Value: id}
		return pane
	}
	probe := testSession{ID: "target", Directory: "/work/acme", Title: "Probe test response"}

	tests := []struct {
		name     string
		sessions []testSession
		messages []testMessage
		pane     provider.Pane
		want     provider.Reading
		wantErr  error
		// justCompacted and clears are what the round records and does with a
		// failure; both are asserted on every case, success included.
		justCompacted bool
		clears        bool
	}{
		{
			name:     "session resolved from the terminal title",
			sessions: []testSession{probe},
			messages: []testMessage{{"answer", "target", "assistant", 1, measured}},
			pane:     titled("OC | Probe test response"),
			want:     provider.Reading{Mode: provider.ResolutionTerminalTitle, Occupancy: 17_298, ModelID: "deepseek-v4-pro"},
		},
		{
			// herdr 0.9.1's opencode integration (v12) reports the root session
			// an opencode2 pane's TUI has selected, as it did for two live panes,
			// each with kind "id"; ses_f187740abffeM9akf27Rp4llEF is one of them.
			// The title names a different real session, so a provider still
			// correlating by title reports the wrong number and the wrong mode.
			name: "reported root session is used ahead of the title",
			sessions: []testSession{
				{ID: "ses_f187740abffeM9akf27Rp4llEF", Directory: "/work/acme", Title: "Fix handoff issues and request agent review"},
				{ID: "ses_other", Directory: "/work/acme", Title: "Probe test response"},
			},
			messages: []testMessage{
				{"reported", "ses_f187740abffeM9akf27Rp4llEF", "assistant", 1, measured},
				{"titled", "ses_other", "assistant", 2, `{"time":{"completed":2},"model":{"id":"deepseek-v4-pro"},"tokens":{"input":1,"cache":{"read":0,"write":0}}}`},
			},
			pane: reported("ses_f187740abffeM9akf27Rp4llEF"),
			want: provider.Reading{Mode: provider.ResolutionAgentSession, Occupancy: 17_298, ModelID: "deepseek-v4-pro"},
		},
		{
			// A sub-agent's session is never the main thread, and this one has a
			// reading, so only the plugin's own root check keeps it off the row.
			// The title names the parent, which must not rescue the pane either.
			name: "reported session is a sub-agent's",
			sessions: []testSession{
				probe,
				{ID: "child", Directory: "/work/acme", Title: "Explore", ParentID: "target"},
			},
			messages: []testMessage{{"answer", "child", "assistant", 1, measured}},
			pane:     reported("child"),
			want:     provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:  ErrNoSession,
			clears:   true,
		},
		{
			name:     "reported session is not in the store",
			sessions: []testSession{probe},
			messages: []testMessage{{"answer", "target", "assistant", 1, measured}},
			pane:     reported("ses_deleted"),
			want:     provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:  ErrNoSession,
			clears:   true,
		},
		{
			name:     "turn in flight keeps the previous completed lower bound",
			sessions: []testSession{probe},
			messages: []testMessage{
				{"previous", "target", "assistant", 1, `{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":120000,"cache":{"read":9000,"write":0}}}`},
				{"in-flight", "target", "assistant", 2, `{"time":{"created":2},"model":{"id":"model"}}`},
			},
			pane: titled("OC | Probe test response"),
			want: provider.Reading{Mode: provider.ResolutionTerminalTitle, Occupancy: 129_000, ModelID: "model"},
		},
		{
			// Resolution succeeded before the read failed, so its mode is still
			// recorded.
			name:     "completed compaction is a boundary",
			sessions: []testSession{probe},
			messages: []testMessage{
				{"pre-compaction", "target", "assistant", 1, `{"time":{"completed":1},"model":{"id":"model"},"tokens":{"input":225000,"cache":{"read":9000,"write":0}}}`},
				{"compaction", "target", "compaction", 2, `{"status":"completed","reason":"auto","summary":"summary","recent":"recent"}`},
				{"in-flight", "target", "assistant", 3, `{"time":{"created":3},"model":{"id":"model"}}`},
			},
			pane:          titled("OC | Probe test response"),
			want:          provider.Reading{Mode: provider.ResolutionTerminalTitle},
			wantErr:       ErrJustCompacted,
			justCompacted: true,
			clears:        true,
		},
		{
			name:     "resolved session has no occupancy yet",
			sessions: []testSession{probe},
			pane:     titled("OC | Probe test response"),
			want:     provider.Reading{Mode: provider.ResolutionTerminalTitle},
			wantErr:  ErrNoOccupancy,
			clears:   true,
		},
		{
			name:     "untitled session",
			sessions: []testSession{probe},
			pane:     titled("OpenCode"),
			wantErr:  opencodeshared.ErrUntitled,
			clears:   true,
		},
		{
			name:     "title matches no session",
			sessions: []testSession{probe},
			messages: []testMessage{{"answer", "target", "assistant", 1, measured}},
			pane:     titled("OC | Missing"),
			wantErr:  opencodeshared.ErrNoTitleMatch,
			clears:   true,
		},
		{
			// A plugin route interpolates its plugin name here, and plugin titles
			// are never shortened, so one this long cannot be a session title.
			// The store cannot tell it from a session title, so the reason is
			// what makes the blank row explainable.
			name:     "title is a plugin route",
			sessions: []testSession{probe},
			pane:     titled("OC | herdr-navigator plugin workspace inspector route"),
			wantErr:  opencodeshared.ErrPluginTitle,
			clears:   true,
		},
		{
			// Measured against opencode2 0.0.0-next-17444: a pane started as
			// `opencode2 mini` reported terminal_title "opencode2 mini", argv0
			// "opencode2" and argv [".../bin/opencode2", "mini"], and herdr still
			// labelled it an opencode agent. Its row staying blank is correct;
			// what this pins is that the error says why, and that attributing the
			// reason leaves the clear alone.
			name:     "pane runs the mini interface",
			sessions: []testSession{probe},
			pane: provider.Pane{
				ForegroundCWD: "/work/acme", TerminalTitle: "opencode2 mini",
				Processes: []provider.Process{{PID: 42, Argv0: "opencode2", Argv: []string{"/nix/store/x-opencode2/bin/opencode2", "mini"}}},
			},
			wantErr: ErrMiniInterface,
			clears:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := newTestDatabase(t)
			db := openFixtureDatabase(t, path)
			for _, session := range tt.sessions {
				insertSession(t, db, session)
			}
			for _, message := range tt.messages {
				insertMessage(t, db, message.id, message.sessionID, message.messageType, message.seq, message.data)
			}
			db.Close()

			assertMeasurement(t, New(path, ""), tt.pane, tt.want, tt.wantErr, tt.justCompacted, tt.clears)
		})
	}
}

// The mode a failed open records is decided by the reported id alone, because
// no store was read: a reported id keeps agent_session, a pane that would have
// been resolved by its title records none.
func TestMeasureRecordsTheModeWhenTheStoreFailsToOpen(t *testing.T) {
	t.Parallel()

	reported := provider.Pane{
		ForegroundCWD: "/work/acme", TerminalTitle: "OC | Probe test response",
		AgentSession: &provider.AgentSession{Kind: "id", Value: "target"},
	}
	titled := provider.Pane{ForegroundCWD: "/work/acme", TerminalTitle: "OC | Probe test response"}
	unwritten := func(t *testing.T) string { return filepath.Join(t.TempDir(), "opencode2.db") }

	tests := []struct {
		name    string
		path    func(*testing.T) string
		pane    provider.Pane
		want    provider.Reading
		wantErr error
		clears  bool
	}{
		{
			name:    "reported id, store not written yet",
			path:    unwritten,
			pane:    reported,
			want:    provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr: sqlitestore.ErrMissing,
			clears:  true,
		},
		{
			name:    "title-resolved pane, store not written yet",
			path:    unwritten,
			pane:    titled,
			wantErr: sqlitestore.ErrMissing,
			clears:  true,
		},
		{
			// A directory where the database should be is not a store that has
			// not been written yet; the failure says nothing about the session,
			// so the displayed value stays.
			name: "title-resolved pane, store is a directory",
			path: func(t *testing.T) string { return t.TempDir() },
			pane: titled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := New(tt.path(t), "")
			got, err := p.Measure(context.Background(), tt.pane)
			if err == nil || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
				t.Fatalf("Measure() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Measure() = %+v, want %+v", got, tt.want)
			}
			if errors.Is(err, provider.ErrJustCompacted) {
				t.Fatalf("Measure() error = %v, want an open failure, not just-compacted", err)
			}
			if clears := provider.IsSuperseded(err, p.Superseded()); clears != tt.clears {
				t.Fatalf("superseded = %t, want %t", clears, tt.clears)
			}
		})
	}
}

// opencode2 does not read OPENCODE_DISABLE_TERMINAL_TITLE itself, but a herdr
// started from a shell exporting it hands it to this process, and that is the
// only channel through which the variable can explain a permanently blank row.
// The process list is positive evidence about the agent while the variable is
// only a hint about our own environment, so a mini pane is attributed to mini:
// blaming the variable would name a cause the user could "fix" without the row
// ever filling in.
func TestMeasureExplainsAnUnavailableTitleByItsStrongestEvidence(t *testing.T) {
	// Not parallel: the variable is process-wide, and the point is that this
	// process inherits it the way the herdr server would.
	t.Setenv("OPENCODE_DISABLE_TERMINAL_TITLE", "1")

	path := newTestDatabase(t)
	db := openFixtureDatabase(t, path)
	insertSession(t, db, testSession{ID: "target", Directory: "/work/acme", Title: "Probe test response"})
	db.Close()

	for _, tt := range []struct {
		name      string
		title     string
		processes []provider.Process
		wantErr   error
	}{
		{
			name:      "ordinary TUI under a suppressed title",
			title:     "zsh",
			processes: []provider.Process{{PID: 42, Argv0: "opencode2"}},
			wantErr:   opencodeshared.ErrTitleDisabled,
		},
		{
			name:      "mini pane under a suppressed title",
			title:     "opencode2 mini",
			processes: []provider.Process{{PID: 42, Argv0: "opencode2", Argv: []string{"/nix/store/x-opencode2/bin/opencode2", "mini"}}},
			wantErr:   ErrMiniInterface,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pane := provider.Pane{ForegroundCWD: "/work/acme", TerminalTitle: tt.title, Processes: tt.processes}
			err := assertMeasurement(t, New(path, ""), pane, provider.Reading{}, tt.wantErr, false, true)
			if tt.wantErr == ErrMiniInterface && errors.Is(err, opencodeshared.ErrTitleDisabled) {
				t.Fatalf("Measure() error = %v, want mini preferred over the environment variable", err)
			}
		})
	}
}

func assertMeasurement(t *testing.T, p provider.Provider, pane provider.Pane, want provider.Reading, wantErr error, justCompacted, clears bool) error {
	t.Helper()
	got, err := p.Measure(context.Background(), pane)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Measure() error = %v, want %v", err, wantErr)
	}
	if got != want {
		t.Fatalf("Measure() = %+v, want %+v", got, want)
	}
	if got := errors.Is(err, provider.ErrJustCompacted); got != justCompacted {
		t.Fatalf("just-compacted = %t, want %t", got, justCompacted)
	}
	if got := provider.IsSuperseded(err, p.Superseded()); got != clears {
		t.Fatalf("superseded = %t, want %t", got, clears)
	}
	return err
}
