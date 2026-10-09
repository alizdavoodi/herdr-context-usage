package opencode

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/alizdavoodi/herdr-context-usage/internal/provider"
	"github.com/alizdavoodi/herdr-context-usage/internal/provider/opencodeshared"
)

// completed is the design document's worked example, 840 / 90000 / 1641 =
// 92,481, as a finished v1 assistant turn.
const completed = `{"role":"assistant","time":{"completed":1},"tokens":{"input":840,"cache":{"read":90000,"write":1641}}}`

// Each case is one pane measured the way a refresh round measures it, asserting
// on everything the round acts on: the reading it reports, whether a failure
// clears the displayed value or keeps it, and whether it is the just-compacted
// state. The completed turn is the design document's worked example, 840 /
// 90000 / 1641 = 92,481; the compaction summary is the real row described in
// reader_test.go.
//
// The session is a root session, and the only one in the store, so a pane that
// resolves nothing proves there is no fallback to the one session available.
func TestMeasureReadsTheResolvedSessionAndClassifiesWhatItFinds(t *testing.T) {
	t.Parallel()

	const zero = `{"role":"assistant","time":{"completed":2},"tokens":{"input":0,"cache":{"read":0,"write":0}}}`
	reported := provider.Pane{AgentSession: &provider.AgentSession{Kind: "id", Value: "ses_current"}}

	tests := []struct {
		name     string
		messages []string
		pane     provider.Pane
		want     provider.Reading
		wantErr  error
		// justCompacted and clears are what the round records and does with a
		// failure; both are asserted on every case, success included.
		justCompacted bool
		clears        bool
	}{
		{
			name:     "reported session",
			messages: []string{completed},
			pane:     reported,
			want:     provider.Reading{Mode: provider.ResolutionAgentSession, Occupancy: 92_481},
		},
		{
			name:     "session resolved from the terminal title",
			messages: []string{completed},
			pane:     provider.Pane{ForegroundCWD: "/work/acme", TerminalTitle: "OC | Probe test response"},
			want:     provider.Reading{Mode: provider.ResolutionTerminalTitle, Occupancy: 92_481},
		},
		{
			// An aborted turn records zeroes, which are no reading at all, and
			// the value on display belongs to a turn this session has moved past.
			name:     "only turn recorded zero occupancy",
			messages: []string{zero},
			pane:     reported,
			want:     provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:  ErrNoOccupancy,
			clears:   true,
		},
		{
			name:     "zero turn after a completed one",
			messages: []string{completed, zero},
			pane:     reported,
			want:     provider.Reading{Mode: provider.ResolutionAgentSession, Occupancy: 92_481},
		},
		{
			// herdr fires an event when an agent starts working, so this is the
			// state a v1 pane is read in for the whole of every turn: a completed
			// reading, the prompt that started the turn, and an assistant row with
			// nothing in it yet.
			name: "turn in flight",
			messages: []string{
				completed,
				`{"role":"user","time":{"created":2},"summary":{"diffs":[]},"agent":"build"}`,
				`{"role":"assistant","mode":"Atlas (Plan Executor)","time":{"created":3}}`,
			},
			pane: reported,
			want: provider.Reading{Mode: provider.ResolutionAgentSession, Occupancy: 92_481},
		},
		{
			// msg_ce8685bc7001SiK5DcsX6T7Q34 of session
			// ses_31c1bfde3ffeCEDhd6W270unu4: the assistant message that wrote a
			// compaction summary. Its own token counts are what summarising the
			// old context cost, not the size of the new one. Resolution succeeded
			// before the read failed, so its mode is still recorded.
			name: "newest message is a compaction summary",
			messages: []string{
				completed,
				`{"role":"assistant","mode":"compaction","agent":"compaction","summary":true,"time":{"created":1773425679303,"completed":1773425736799},"tokens":{"input":2,"cache":{"read":0,"write":131757}}}`,
			},
			pane:          reported,
			want:          provider.Reading{Mode: provider.ResolutionAgentSession},
			wantErr:       ErrJustCompacted,
			justCompacted: true,
			clears:        true,
		},
		{
			name:     "no reported session and no title",
			messages: []string{completed},
			pane:     provider.Pane{},
			wantErr:  opencodeshared.ErrTitleUnavailable,
			clears:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := fixtureDatabase(t, func(database *sql.DB) {
				exec(t, database, `INSERT INTO session (id, directory, title) VALUES ('ses_current', '/work/acme', 'Probe test response')`)
				for i, data := range tt.messages {
					exec(t, database, `INSERT INTO message (id, session_id, time_created, data) VALUES (?, 'ses_current', ?, ?)`, i, i+1, data)
				}
			})

			p := New(path, "")
			got, err := p.Measure(context.Background(), tt.pane)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Measure() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Measure() = %+v, want %+v", got, tt.want)
			}
			if justCompacted := errors.Is(err, provider.ErrJustCompacted); justCompacted != tt.justCompacted {
				t.Fatalf("just-compacted = %t, want %t", justCompacted, tt.justCompacted)
			}
			if clears := provider.IsSuperseded(err, p.Superseded()); clears != tt.clears {
				t.Fatalf("superseded = %t, want %t", clears, tt.clears)
			}
		})
	}
}
