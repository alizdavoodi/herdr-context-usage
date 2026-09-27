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
	PID   int      `json:"pid"`
	Argv0 string   `json:"argv0"`
	Argv  []string `json:"argv"`
}

// FromAgent chooses a pane's kind from herdr's agent label. processes is read
// only for opencode, whose label hosts both versions; NeedsProcessList says why
// a list that could not be read must never reach here.
func FromAgent(agent string, processes []Process) Kind {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "claude":
		return Claude
	case "codex":
		return Codex
	// herdr labels pi and omp panes on their own, unlike the opencode pair whose
	// version only the foreground process reveals, so no process inspection
	// belongs here.
	case "pi":
		return Pi
	case "omp":
		return Omp
	// Hermes runs as python3.12, so its process names nothing; herdr's label is
	// the only signal, and processKind deliberately does not learn it.
	case "hermes":
		return Hermes
	case "opencode2":
		return OpenCode2
	case "opencode":
		for _, process := range processes {
			if processKind(process) == OpenCode2 {
				return OpenCode2
			}
		}
		return OpenCode
	}
	return None
}

// NeedsProcessList reports whether classifying an agent depends on its pane's
// process list. Only opencode does: the one label hosts both v1 and v2, and
// the process list is the only way to tell them apart. An observed empty list
// is evidence of v1, and a guess would either report nonsense or blank a live
// value, so an opencode pane whose list could not be read must not be
// classified at all: the caller skips it for the round and keeps its displayed
// value.
func NeedsProcessList(agent string) bool {
	return strings.ToLower(strings.TrimSpace(agent)) == "opencode"
}

func ProcessIDs(processes []Process, kind Kind) []int {
	ids := make([]int, 0, 1)
	for _, process := range processes {
		if processKind(process) == kind {
			ids = append(ids, process.PID)
		}
	}
	return ids
}

// Kind reports which agent a single process is, by the same basename rule
// dispatch uses to choose a provider for the pane. Exported so a provider can
// find its own entry in a pane's process list without restating that rule.
func (p Process) Kind() Kind { return processKind(p) }

func processKind(process Process) Kind {
	name := strings.ToLower(strings.TrimPrefix(filepath.Base(process.Argv0), "."))
	switch name {
	case "opencode2":
		return OpenCode2
	case "opencode":
		return OpenCode
	case "claude":
		return Claude
	case "codex":
		return Codex
	default:
		return None
	}
}
