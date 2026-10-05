package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
		ContextWindow: 5000, CompactRatio: 0.5, RecentKeep: 2, ArchiveDir: testenv.TempDir(t),
	}, sink)
	return a, sink
}

// An automatic fold that fails has to say which class of failure it was on both
// surfaces a user reads: the receipt that blocks the generation, and the card
// that otherwise reads "folded nothing".
func TestFailedAutomaticFoldCarriesATypedCause(t *testing.T) {
	a, sink := failureFixture(t, &fakeProvider{streamErr: errors.New("provider down")})
	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	r := a.sess.win.compactionState.LastReceipt
	if r == nil || r.Code != string(FailSummaryFailed) {
		t.Fatalf("receipt = %+v, want code %q", r, FailSummaryFailed)
	}
	var aborted []string
	for _, e := range sink.kinds(event.CompactionDone) {
		aborted = append(aborted, e.Compaction.Code)
	}
	if len(aborted) != 1 || aborted[0] != string(FailSummaryFailed) {
		t.Fatalf("aborted card codes = %v, want [%s]", aborted, FailSummaryFailed)
	}
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

func TestFailureCodeFollowsTheSentinelThroughWrapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want CompactionNoopReason
	}{
		{nil, ""},
		{fmt.Errorf("x: %w", errSummaryOutputTruncated), FailSummaryTruncated},
		{fmt.Errorf("x: %w", errCompressStaleContext), FailContextChanged},
		{fmt.Errorf("x: %w", errSummaryTimeout), FailSummaryTimeout},
		{fmt.Errorf("x: %w", context.DeadlineExceeded), FailUnclassified},
		{fmt.Errorf("x: %w", context.Canceled), FailCancelled},
		{fmt.Errorf("x: %w", errCompactionHookRefused), FailHookRefused},
		{fmt.Errorf("x: %w", errProjectionNotPersisted), FailPersistFailed},
		{fmt.Errorf("x: %w", errSummaryRequestFailed), FailSummaryFailed},
		{fmt.Errorf("x: %w", errSummaryInputTooLarge), FailSummaryInputTooLarge},
		{errors.New("unclassified"), FailUnclassified},
		{rejectCheckpoint(NoopFixedPrefixAboveTrigger, "fixed prefix"), NoopFixedPrefixAboveTrigger},
	} {
		if got := compactionFailureCode(tc.err); got != tc.want {
			t.Errorf("%v: code = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// Only the summarizer's own deadline is a summary timeout: the caller's
// context expiring is not the summarizer's fault.
func TestOnlyTheSummarizersOwnDeadlineIsASummaryTimeout(t *testing.T) {
	live := context.Background()
	if err := classifySummaryError(live, context.DeadlineExceeded); !errors.Is(err, errSummaryTimeout) {
		t.Fatalf("own deadline = %v, want errSummaryTimeout", err)
	}
	expired, cancel := context.WithDeadline(live, time.Now().Add(-time.Second))
	defer cancel()
	if err := classifySummaryError(expired, context.DeadlineExceeded); errors.Is(err, errSummaryTimeout) || errors.Is(err, errSummaryRequestFailed) {
		t.Fatalf("caller deadline = %v, must not be attributed to the summarizer", err)
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

func TestPersistenceFailureIsNamedNotBlamedOnTheSummary(t *testing.T) {
	a, sink := failureFixture(t, &fakeProvider{reply: "digest"})
	file := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a.sess.path = filepath.Join(file, "session.jsonl")
	_, _, err := a.window().compactToProjection(context.Background(), CompactionTriggerPressure, "", compactionScope{}, false)
	if !errors.Is(err, errProjectionNotPersisted) || compactionFailureCode(err) != FailPersistFailed {
		t.Fatalf("err = %v, want a persistence failure", err)
	}
	done := sink.kinds(event.CompactionDone)
	if len(done) == 0 || done[len(done)-1].Compaction.Code != string(FailPersistFailed) {
		t.Fatalf("aborted frame = %+v, want code %s", done, FailPersistFailed)
	}
}

// A fold the caller cancelled is not an unexplained one: the card names it.
func TestCancelledAutomaticFoldCarriesACancellationCode(t *testing.T) {
	a, sink := failureFixture(t, &fakeProvider{streamErr: context.Canceled})
	_ = prepareContext(context.Background(), a, CompactionTriggerPressure)
	var aborted []string
	for _, e := range sink.kinds(event.CompactionDone) {
		aborted = append(aborted, e.Compaction.Code)
	}
	if len(aborted) != 1 || aborted[0] != string(FailCancelled) {
		t.Fatalf("aborted card codes = %v, want [%s]", aborted, FailCancelled)
	}
}
