package report

import "errors"

// Class is what a failed reading proves about the context occupancy already
// displayed for a pane. Conflating Superseded with StaleLow blanks live panes;
// conflating StaleLow with Transient makes a permission error look like a
// lifecycle state. Both mistakes have shipped here, which is why the three are
// named separately and decided in one place.
type Class int

const (
	// Transient — we could not tell. Preserve the displayed value: a permission
	// or I/O failure says nothing about the session.
	Transient Class = iota
	// Superseded — the displayed value is wrong. Clear it. Just-compacted, no
	// session, or a session that has produced no reading at all.
	Superseded
	// StaleLow — the displayed value is merely behind. Preserve it. Occupancy
	// only rises within a turn, so the previous reading is a valid lower bound.
	StaleLow
)

// States is one provider's declaration of which of its own answers fall into
// which class. Anything a provider does not declare is transient, so a new
// error defaults to preserving the displayed value rather than blanking a pane.
//
// No answer may appear in both lists: an answer is either wrong or behind, and
// Classify resolves a contradiction towards Superseded because presenting a
// superseded value as current is the failure this whole seam exists to prevent.
type States struct {
	Superseded []error
	StaleLow   []error
}

// Classify decides what err proves about the displayed value. Providers express
// unknown readings as errors so every successful numeric reading, including
// zero, remains valid.
func Classify(err error, states States) Class {
	for _, state := range states.Superseded {
		if errors.Is(err, state) {
			return Superseded
		}
	}
	for _, state := range states.StaleLow {
		if errors.Is(err, state) {
			return StaleLow
		}
	}
	return Transient
}
