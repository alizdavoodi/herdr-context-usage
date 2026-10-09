package provider

import (
	"path/filepath"
	"strings"
)

type Kind string

const (
	None      Kind = ""
	Claude    Kind = "claude"
	Codex     Kind = "codex"
	OpenCode  Kind = "opencode"
	OpenCode2 Kind = "opencode2"
	// herdr's agent kinds are a closed enumeration compiled into its binary and
	// a user cannot extend it, so this spelling is load-bearing: a misspelled
	// constant matches no agent label and produces a permanently blank row with
	// no error anywhere to explain it. "oh-my-pi" is not a value, and neither is
	// any transposition of "omp".
	Pi Kind = "pi"
	// herdr labels omp separately from pi even though omp is built on pi, so
	// the two are distinct kinds here as well. The same spelling warning
	// applies, and more sharply: "oh-my-pi" is what the project calls itself
	// in prose, and it is not the label.
	Omp Kind = "omp"
	// The same load-bearing spelling as pi and omp: "hermes-agent" is the
	// package's name, and it is not the label.
	Hermes Kind = "hermes"
)

type Process struct {
	PID int `json:"pid"`
	// The herdr client fills this from argv[0] when herdr omits it.
	Argv0 string   `json:"argv0"`
	Argv  []string `json:"argv"`
}

// FromAgent chooses a pane's kind from herdr's agent label. processes is read
// only for opencode, whose label hosts both versions. ok is false when that
// choice cannot be made this round, which happens only for opencode whose
// list could not be read: an observed empty list is evidence of v1, but a
// failed read is evidence of nothing, and a guess would either report the
// wrong store or blank a live value, so the caller keeps the pane's value.
// Every other label ignores processErr.
func FromAgent(agent string, processes []Process, processErr error) (Kind, bool) {
	switch normalizeAgent(agent) {
	case "claude":
		return Claude, true
	case "codex":
		return Codex, true
	// herdr labels pi and omp panes on their own, unlike the opencode pair whose
	// version only the foreground process reveals, so no process inspection
	// belongs here.
	case "pi":
		return Pi, true
	case "omp":
		return Omp, true
	// Hermes runs as python3.12, so its process names nothing; herdr's label is
	// the only signal, and Process.Kind deliberately does not learn it.
	case "hermes":
		return Hermes, true
	case "opencode2":
		return OpenCode2, true
	case "opencode":
		if processErr != nil {
			return None, false
		}
		for _, process := range processes {
			if process.Kind() == OpenCode2 {
				return OpenCode2, true
			}
		}
		return OpenCode, true
	}
	return None, true
}

// ReadsProcesses reports whether anything downstream of an agent label reads
// its pane's process list, so the caller pays herdr's process-info subprocess
// only where the answer is used:
//   - opencode, because the one label hosts both v1 and v2 and the foreground
//     process is the only way FromAgent can tell them apart.
//   - claude, because its process ids key the registry resolution tier.
//   - opencode2, because a mini-interface failure is attributed from the
//     process list.
//
// pi, omp and hermes are labelled by herdr on their own and none of their
// providers looks at processes, so they never fetch. A new provider that reads
// Pane.Processes must add its label here, or it silently receives nil.
func ReadsProcesses(agent string) bool {
	switch normalizeAgent(agent) {
	case "claude", "opencode", "opencode2":
		return true
	}
	return false
}

func normalizeAgent(agent string) string {
	return strings.ToLower(strings.TrimSpace(agent))
}

// Kind reports which agent a single process is, by the same basename rule
// dispatch uses to choose a provider for the pane. Exported so a provider can
// find its own entry in a pane's process list without restating that rule. It
// names only the kinds some caller asks about: opencode2, which tells the
// opencode versions apart, and claude, whose process ids key its registry
// tier.
func (p Process) Kind() Kind {
	switch strings.ToLower(strings.TrimPrefix(filepath.Base(p.Argv0), ".")) {
	case "opencode2":
		return OpenCode2
	case "claude":
		return Claude
	default:
		return None
	}
}
