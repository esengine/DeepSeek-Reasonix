package event

// FanOut dispatches each event to every registered sink in order.
// A nil sink in the list is silently skipped. Use it when you want one
// event stream to reach multiple consumers — e.g. the desktop tab UI and
// a bot-channel notifier.
type FanOut struct {
	sinks []Sink
}

// NewFanOut returns a FanOut that delivers every Emit call to every sink
// in the given order. A zero-length list is valid (no-op).
func NewFanOut(sinks ...Sink) *FanOut {
	return &FanOut{sinks: sinks}
}

// Emit forwards e to every registered sink. Nil sinks are skipped.
func (f *FanOut) Emit(e Event) {
	for _, s := range f.sinks {
		if s == nil {
			continue
		}
		s.Emit(e)
	}
}

// Len returns the number of registered sinks.
func (f *FanOut) Len() int { return len(f.sinks) }
