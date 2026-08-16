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
)

type Process struct {
	PID   int      `json:"pid"`
	Argv0 string   `json:"argv0"`
	Argv  []string `json:"argv"`
}

func FromAgent(agent string, processes []Process) Kind {
	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "claude":
		return Claude
	case "codex":
		return Codex
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
	return k == Claude
}
