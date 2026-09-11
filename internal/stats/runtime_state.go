package stats

import "reasonix/internal/event"

func (r *Recorder) RuntimeStateChanged(snapshot event.RuntimeStateSnapshot) {
	if r != nil {
		event.PublishRuntimeState(r.inner, snapshot)
	}
}
