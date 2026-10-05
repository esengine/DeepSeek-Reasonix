package agent

import "reasonix/internal/contract/event"

// producerSink names the model that produced each frame an agent emits. A
// two-model turn puts two producers on one stream, and the only other way to
// tell them apart is the last phase marker before the frame — an adjacency,
// not a fact, and one no replay off the record can even see. A forwarder that
// re-parents another agent's work relabels on the way through.
type producerSink struct {
	event.AuditForwarder
	inner  event.Sink
	source string
}

func (s producerSink) Emit(e event.Event) {
	// A delta is a fragment of a message whose settled frame names its producer,
	// so naming it says nothing new — and costs the stream: coalescing merges
	// only deltas carrying nothing but text.
	switch e.Kind {
	case event.Text, event.Reasoning, event.CompactionProgress:
	default:
		if e.Source == "" {
			e.Source = s.source
		}
	}
	s.inner.Emit(e)
}

// withProducer labels sink's frames with source. An empty source leaves the
// sink alone: an agent nobody named a role for names none.
func withProducer(sink event.Sink, source string) event.Sink {
	if source == "" {
		return sink
	}
	return producerSink{AuditForwarder: event.AuditForwarder{Inner: sink}, inner: sink, source: source}
}
