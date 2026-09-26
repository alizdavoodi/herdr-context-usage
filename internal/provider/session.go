package provider

import (
	"path/filepath"
	"strings"
)

// AgentSession is what herdr reports about the agent session a pane is
// displaying: a label and a value, and nothing else this plugin reads.
//
// herdr's report also names the integration that sent it and the agent it is
// for. Neither is decoded: the pane's own agent label has already chosen the
// provider, and neither field changes how the value is read, so they are left
// for the decoder to drop rather than carried unread.
type AgentSession struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Path reports the value as a transcript path, when it is one.
func (s *AgentSession) Path() (string, bool) {
	if s.interpret() != asPath {
		return "", false
	}
	return s.Value, true
}

// ID reports the value as a session id, when it is one.
func (s *AgentSession) ID() (string, bool) {
	if s.interpret() != asID {
		return "", false
	}
	return s.Value, true
}

type interpretation int

const (
	refused interpretation = iota
	asPath
	asID
)

// interpret is the one reading of a reported value, shared by every provider
// so that no two of them can disagree about what herdr said. Each rule leans
// the way ADR-0004 does, towards a blank row over a confident wrong number.
//
// A path is used only when it is absolute. A relative one, labelled or not,
// would be joined against whatever directory herdr launched this plugin in, so
// a same-named file sitting there would be read as the transcript; the label
// buys no trust the value does not carry. Nothing observed reports one: a live
// pi pane reports kind "path" with a full transcript path, and omp's
// integration reports an absolute path whenever it has one and a session id
// otherwise.
//
// A value labelled an id is never read as a path, whatever it looks like: the
// agent said what it is, and opening it as a file would read a path it never
// claimed was one.
//
// An unlabelled value is a path when absolute and an id otherwise, where each
// provider's own match resolves an absent or non-unique id to nothing rather
// than to a guess.
//
// herdr's labels are a closed set, so one outside it is a change this plugin
// has not seen, and it is refused rather than read as the nearest known one.
// Case and surrounding space are not part of a label's meaning and are
// ignored.
func (s *AgentSession) interpret() interpretation {
	if s == nil || s.Value == "" {
		return refused
	}
	switch strings.ToLower(strings.TrimSpace(s.Kind)) {
	case "path":
		if filepath.IsAbs(s.Value) {
			return asPath
		}
		return refused
	case "id":
		return asID
	case "":
		if filepath.IsAbs(s.Value) {
			return asPath
		}
		return asID
	default:
		return refused
	}
}
