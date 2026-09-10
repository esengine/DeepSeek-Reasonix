package event

import "reasonix/internal/nilutil"

// Sink consumes a turn's events. The agent calls Emit serially from its run
// loop (tool execution may fan out across goroutines, but emission does not),
// so an implementation need not be safe for concurrent Emit. Emit must not
// block indefinitely — a channel-backed sink should be buffered or drained by
// a live reader.
type Sink interface {
	Emit(Event)
}

// CheckedSink is an optional durability-aware sink capability. Callers use it
// at side-effect boundaries (tool dispatch, user prompts, terminal commits)
// where continuing after a local journal failure would make runtime state
// impossible to recover safely. Ordinary display-only sinks keep implementing
// Sink; EmitChecked falls back to Emit for compatibility.
type CheckedSink interface {
	EmitChecked(Event) error
}

// EmitChecked emits e and returns a durability failure when the sink exposes
// CheckedSink. It deliberately does not make every Sink fallible: most event
// consumers are renderers, while the session lifecycle decorator is the one
// owner that can provide a durable acknowledgement.
func EmitChecked(s Sink, e Event) error {
	if nilutil.IsNil(s) {
		return nil
	}
	if checked, ok := s.(CheckedSink); ok {
		return checked.EmitChecked(e)
	}
	s.Emit(e)
	return nil
}

// FuncSink adapts a plain function to a Sink.
type FuncSink func(Event)

// Emit calls the wrapped function.
func (f FuncSink) Emit(e Event) {
	if f != nil {
		f(e)
	}
}

// Discard is a Sink that drops every event. Useful in tests and for runs that
// only care about the final session state.
var Discard Sink = FuncSink(func(Event) {})
