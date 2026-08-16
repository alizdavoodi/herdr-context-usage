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

func FromProcesses(processes []Process) Kind {
	for _, process := range processes {
		name := strings.ToLower(strings.TrimPrefix(filepath.Base(process.Argv0), "."))
		// Helper processes commonly borrow provider names as prefixes.
		switch name {
		case "opencode2":
			return OpenCode2
		case "opencode":
			return OpenCode
		case "claude":
			return Claude
		case "codex":
			return Codex
		}
	}
	return None
}

func (k Kind) Implemented() bool {
	return k == Claude
}
