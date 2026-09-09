package event

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

type runtimeStateCaptureSink struct {
	states chan RuntimeStateSnapshot
	events atomic.Int32
}

func (s *runtimeStateCaptureSink) Emit(Event)                                     { s.events.Add(1) }
func (s *runtimeStateCaptureSink) RuntimeStateChanged(state RuntimeStateSnapshot) { s.states <- state }

func TestRuntimeStateForwardsOutsideTranscriptEvents(t *testing.T) {
	for _, kind := range []string{"direct", "sync", "coalesce", "audit", "combined"} {
		t.Run(kind, func(t *testing.T) {
			capture := &runtimeStateCaptureSink{states: make(chan RuntimeStateSnapshot, 1)}
			var sink Sink = capture
			switch kind {
			case "sync":
				sink = Sync(sink)
			case "coalesce":
				sink = Coalesce(sink, time.Millisecond)
			case "audit":
				sink = runtimeAuditTestSink{AuditForwarder: AuditForwarder{Inner: sink}}
			case "combined":
				sink = Sync(Coalesce(runtimeAuditTestSink{AuditForwarder: AuditForwarder{Inner: sink}}, time.Millisecond))
			}
			want := RuntimeStateSnapshot{SchemaVersion: 1, RuntimeEpoch: "test-runtime", Revision: 9, Phase: "finishing", Running: true, TurnID: "test-turn"}
			PublishRuntimeState(sink, want)
			select {
			case got := <-capture.states:
				if got != want {
					t.Fatalf("wrapper changed snapshot: got=%+v want=%+v", got, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("wrapper swallowed runtime capability")
			}
			if capture.events.Load() != 0 {
				t.Fatal("runtime notification entered the transcript event channel")
			}
		})
	}
}

type runtimeAuditTestSink struct{ AuditForwarder }

func (s runtimeAuditTestSink) Emit(e Event) { s.Inner.Emit(e) }

func TestRuntimeStateJSONExplicitZeroValues(t *testing.T) {
	raw, err := json.Marshal(RuntimeStateSnapshot{SchemaVersion: 1, RuntimeEpoch: "test", Revision: 1, Phase: "idle"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"running", "pendingPrompt", "cancelRequested", "cancellable"} {
		if value, ok := fields[key]; !ok || value != false {
			t.Fatalf("%s must explicitly clear stale state: %s", key, raw)
		}
	}
	if value, ok := fields["backgroundJobs"]; !ok || value != float64(0) {
		t.Fatalf("backgroundJobs must explicitly clear stale count: %s", raw)
	}
}

func TestRuntimeStatePublishAcceptsLegacyAndNilSinks(t *testing.T) {
	var typedNil *runtimeStateCaptureSink
	for _, sink := range []Sink{nil, typedNil, Discard, FuncSink(func(Event) { t.Fatal("legacy sink received a synthetic event") })} {
		PublishRuntimeState(sink, RuntimeStateSnapshot{Phase: "idle"})
	}
}
