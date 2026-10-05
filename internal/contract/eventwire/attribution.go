package eventwire

import "reasonix/internal/contract/event"

// wireModelRef is the model a frontend may attribute a frame to: the turn's on
// a start, a second model's on the text it wrote. The turn's own chunks carry
// none, because the start already said it.
func wireModelRef(e event.Event) string {
	switch e.Kind {
	case event.TurnStarted:
		return e.ModelRef
	case event.Text, event.Message:
		if e.Source != "" {
			return e.ModelRef
		}
	}
	return ""
}
