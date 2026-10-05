package agent

import "sync/atomic"

// requestModeState is the model mode id every main-loop request carries. The
// controller validates it against the model's declarations; the adapter still
// sends only what its endpoint declared.
type requestModeState struct{ id atomic.Pointer[string] }

func (s *requestModeState) set(id string) {
	if id == "" {
		s.id.Store(nil)
		return
	}
	s.id.Store(&id)
}

func (s *requestModeState) get() string {
	if p := s.id.Load(); p != nil {
		return *p
	}
	return ""
}

func (s *requestModeState) reset() { s.id.Store(nil) }

// SetRequestMode selects the model mode later requests carry; "" turns it off.
// It rides the request, never the prompt, so the cached prefix is unchanged.
func (a *Agent) SetRequestMode(id string) { a.sess.mode.set(id) }

// RequestMode is the model mode the next request carries, "" when off.
func (a *Agent) RequestMode() string { return a.sess.mode.get() }
