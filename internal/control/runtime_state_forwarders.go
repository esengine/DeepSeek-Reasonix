package control

import "reasonix/internal/event"

func (s *inboxEventSink) RuntimeStateChanged(snapshot event.RuntimeStateSnapshot) {
	if s != nil {
		event.PublishRuntimeState(s.inner, snapshot)
	}
}
func (s *frontendEventSink) RuntimeStateChanged(snapshot event.RuntimeStateSnapshot) {
	if s != nil {
		event.PublishRuntimeState(s.inner, snapshot)
	}
}
func (s *turnEventSink) RuntimeStateChanged(snapshot event.RuntimeStateSnapshot) {
	if s != nil {
		event.PublishRuntimeState(s.innerSnapshot(), snapshot)
	}
}
func (s *turnEventDurableSink) RuntimeStateChanged(snapshot event.RuntimeStateSnapshot) {
	if s != nil {
		event.PublishRuntimeState(s.inner(), snapshot)
	}
}
