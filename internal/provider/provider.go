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
)

type Process struct {
	PID   int      `json:"pid"`
	Argv0 string   `json:"argv0"`
	Argv  []string `json:"argv"`
}

type ProcessList struct {
	Entries []Process
	// Available keeps a failed lookup distinct from an observed empty list;
	// only the latter is evidence that an opencode pane is running v1.
	Available bool
}

func FromAgent(agent string, processes ProcessList) Kind {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "claude":
		return Claude
	case "codex":
		return Codex
	// herdr labels a pi pane on its own, unlike the opencode pair whose version
	// only the foreground process reveals, so no process inspection belongs here.
	case "pi":
		return Pi
	case "opencode2":
		return OpenCode2
	case "opencode":
		if !processes.Available {
			return None
		}
		for _, process := range processes.Entries {
			if processKind(process) == OpenCode2 {
				return OpenCode2
			}
		}
		return OpenCode
	}
	return None
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

func (k Kind) Implemented() bool {
	return k == Claude || k == Codex || k == OpenCode || k == OpenCode2 || k == Pi
}
