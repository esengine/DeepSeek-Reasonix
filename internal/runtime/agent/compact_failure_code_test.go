package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/protocol"
)

func failureFixture(t *testing.T, prov provider.Provider) (*Agent, *recordSink) {
	t.Helper()
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), foldableSessionOverForce(6), Options{
		ContextWindow: 5000, CompactRatio: 0.5, ArchiveDir: testenv.TempDir(t),
	}, sink)
	return a, sink
}

// A candidate the host refuses names which rule refused it. Four rules share one
// sentinel, and a reader that cannot tell them apart reports all as the first.
func TestCheckpointRejectionNamesTheRule(t *testing.T) {
	a, _ := failureFixture(t, &fakeProvider{reply: "digest"})
	w := a.window()
	trigger := w.compactTrigger()
	for _, tc := range []struct {
		name                     string
		source, candidate, fixed int
		want                     CompactionNoopReason
	}{
		{"not smaller", 1000, 1000, 10, NoopCandidateNotSmaller},
		{"still at trigger", trigger * 4, trigger, 10, NoopCandidateAboveTrigger},
	} {
		err := w.acceptCheckpointCandidate(CompactionTriggerPressure, compactionScope{}, tc.source, tc.candidate, tc.fixed)
		if got := compactionFailureCode(err); got != tc.want {
			t.Errorf("%s: code = %q, want %q (err %v)", tc.name, got, tc.want, err)
		}
	}
}

func TestHookRefusalIsNamedAtTheFoldItRefused(t *testing.T) {
	a, sink := failureFixture(t, &fakeProvider{reply: "digest"})
	client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "policy"}, nil
	}}
	a.SetExtensions(newExtSlotDispatcher(client, false, nil,
		[]extension.InterceptorPoint{extension.PointCompactionPrepare}, map[extension.Slot]string{}))
	_, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerPressure, "", compactionScope{}, false)
	if !errors.Is(err, errCompactionHookRefused) || compactionFailureCode(err) != FailHookRefused {
		t.Fatalf("err = %v, want a hook refusal", err)
	}
	done := sink.kinds(event.CompactionDone)
	if len(done) == 0 || done[len(done)-1].Compaction.Code != string(FailHookRefused) {
		t.Fatalf("aborted frame = %+v, want code %s", done, FailHookRefused)
	}
}
