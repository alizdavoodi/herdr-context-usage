package provider

import "testing"

func TestAgentSessionReadsItsValueByOneRule(t *testing.T) {
	t.Parallel()

	const (
		absolute = "/work/sessions/2026-08-18T12-34-48-971Z_01a014de.jsonl"
		id       = "01a014de-020a-70b2-bc8e-b6a70ed3e784"
	)

	// An empty want means the method reports false: no real path or id is empty.
	tests := []struct {
		name     string
		session  *AgentSession
		wantPath string
		wantID   string
	}{
		{name: "nothing reported", session: nil},
		{name: "a path label with no value", session: &AgentSession{Kind: "path"}},
		{name: "an id label with no value", session: &AgentSession{Kind: "id"}},
		{name: "no label and no value", session: &AgentSession{}},

		{name: "an absolute path", session: &AgentSession{Kind: "path", Value: absolute}, wantPath: absolute},
		// A relative path would be joined against whatever directory herdr
		// launched this plugin in, so the label buys it no trust.
		{name: "a bare file name labelled path", session: &AgentSession{Kind: "path", Value: "session.jsonl"}},
		{name: "a nested relative path", session: &AgentSession{Kind: "path", Value: "sessions/work/session.jsonl"}},
		{name: "a dot-relative path", session: &AgentSession{Kind: "path", Value: "./session.jsonl"}},
		{name: "a parent-relative path", session: &AgentSession{Kind: "path", Value: "../session.jsonl"}},

		{name: "an id", session: &AgentSession{Kind: "id", Value: id}, wantID: id},
		// Labelled an id, an absolute-looking value is still an id: reading it
		// as a file would open a path the agent never claimed was one.
		{name: "an id that looks like a path", session: &AgentSession{Kind: "id", Value: absolute}, wantID: absolute},

		{name: "a title-case path label", session: &AgentSession{Kind: "Path", Value: absolute}, wantPath: absolute},
		{name: "an upper-case path label", session: &AgentSession{Kind: "PATH", Value: absolute}, wantPath: absolute},
		{name: "an upper-case id label on a path-like value", session: &AgentSession{Kind: "ID", Value: absolute}, wantID: absolute},
		{name: "a padded id label", session: &AgentSession{Kind: " id ", Value: id}, wantID: id},

		{name: "an unlabelled absolute value", session: &AgentSession{Value: absolute}, wantPath: absolute},
		{name: "an unlabelled id", session: &AgentSession{Value: id}, wantID: id},
		{name: "an unlabelled relative path", session: &AgentSession{Value: "session.jsonl"}, wantID: "session.jsonl"},

		{name: "an unknown label on a non-absolute value", session: &AgentSession{Kind: "unknown", Value: id}},
		{name: "an unknown label on an absolute value", session: &AgentSession{Kind: "unknown", Value: absolute}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path, isPath := tt.session.Path()
			if path != tt.wantPath || isPath != (tt.wantPath != "") {
				t.Errorf("Path() = (%q, %t), want (%q, %t)", path, isPath, tt.wantPath, tt.wantPath != "")
			}
			id, isID := tt.session.ID()
			if id != tt.wantID || isID != (tt.wantID != "") {
				t.Errorf("ID() = (%q, %t), want (%q, %t)", id, isID, tt.wantID, tt.wantID != "")
			}
		})
	}
}
