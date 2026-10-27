package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/observation"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/trustedstate"
)

type finalizationProvider struct {
	before func()
	calls  atomic.Int64
}

func (*finalizationProvider) Name() string { return "finalization-test" }

func (p *finalizationProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls.Add(1)
	if p.before != nil {
		p.before()
	}
	out := make(chan provider.Chunk, 2)
	out <- provider.Chunk{Type: provider.ChunkText, Text: "The answer is complete."}
	out <- provider.Chunk{Type: provider.ChunkDone}
	close(out)
	return out, nil
}

type heldFinalIndex struct {
	*trustedstate.Store
	calls   atomic.Int64
	started chan struct{}
	blocked chan struct{}
	release chan struct{}
	exited  chan struct{}
}

func (s *heldFinalIndex) PutIndexObject(b []byte) (trustedstate.Digest, error) {
	switch s.calls.Add(1) {
	case 1:
		defer close(s.started)
	case 2:
		close(s.blocked)
		<-s.release
		defer close(s.exited)
	}
	return s.Store.PutIndexObject(b)
}

func TestFinalObservationIsBoundedAfterACompletedAnswer(t *testing.T) {
	for _, end := range []string{"cancel", "deadline", "observation_budget"} {
		t.Run(end, func(t *testing.T) {
			store := &heldFinalIndex{Store: trustedstate.Open(t.TempDir(), nil), started: make(chan struct{}), blocked: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
			var release sync.Once
			defer func() {
				release.Do(func() { close(store.release) })
				<-store.exited
			}()
			root := t.TempDir()
			p := &finalizationProvider{before: func() {
				<-store.started
				if err := os.WriteFile(filepath.Join(root, "external.txt"), []byte("changed outside the agent"), 0o600); err != nil {
					t.Error(err)
				}
			}}
			seal := &EvidenceSeal{Store: store.Store, Stream: "ws", Root: root, Observer: observation.NewObserver(store, observation.DefaultPolicy())}
			sess := sessionstore.NewSession("")
			sink := &contractAuditSink{}
			a := New(p, tool.NewRegistry(), sess, Options{EvidenceSeal: seal}, sink)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var want error
			// Run still seals the bundle once its context ends, so the wait
			// only catches an observation holding the turn, never latency.
			wait := testenv.Budget(t)
			if end == "cancel" {
				want = context.Canceled
			}
			if end == "deadline" {
				ctx, cancel = context.WithTimeout(t.Context(), 2*time.Second)
				defer cancel()
				want = context.DeadlineExceeded
			}
			if end == "observation_budget" {
				wait += snapshotWait
			}
			done := make(chan error, 1)
			go func() { done <- a.Run(ctx, "Answer the question.") }()
			select {
			case <-store.blocked:
			case <-time.After(testenv.Budget(t)):
				t.Fatal("final workspace observation was not reached")
			}
			msgs := sess.Snapshot()
			if len(msgs) == 0 || msgs[len(msgs)-1].Content != "The answer is complete." {
				t.Fatalf("answer was not committed before final observation: %+v", msgs)
			}
			if end == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if err != nil || !errors.Is(ctx.Err(), want) {
					t.Fatalf("completed Run error = %v, context = %v, want nil and %v", err, ctx.Err(), want)
				}
			case <-time.After(wait):
				release.Do(func() { close(store.release) })
				<-done
				t.Fatal("completed answer still held the turn in workspace observation after its context ended")
			}
			if p.calls.Load() != 1 {
				t.Fatalf("model requests = %d, want 1", p.calls.Load())
			}
			if end == "observation_budget" {
				audit := sink.last(t)
				if !audit.Sealed || audit.SnapshotComplete {
					t.Fatalf("timed-out observation audit = %+v", audit)
				}
				record, err := store.Record(trustedstate.Digest(audit.Record))
				if err != nil {
					t.Fatal(err)
				}
				payload, err := store.Object(record.Payload)
				if err != nil {
					t.Fatal(err)
				}
				var obs workspaceObservation
				if err := json.Unmarshal(payload, &obs); err != nil {
					t.Fatal(err)
				}
				if obs.After != nil || obs.WithinTurn.Compared || obs.WithinTurn.Reason != observation.IncompleteCancelled {
					t.Fatalf("timed-out observation claimed a comparison: %+v", obs)
				}
				p.before = nil
				if err := a.Run(t.Context(), "Answer a follow-up."); err != nil {
					t.Fatalf("follow-up while old observation is blocked: %v", err)
				}
				if p.calls.Load() != 2 {
					t.Fatalf("follow-up model requests = %d, want 2", p.calls.Load())
				}
			}
		})
	}
}

type finalizationSink struct {
	contractAuditSink
	report chan struct{}
}

func (s *finalizationSink) RecordCompletionReport(event.CompletionReportAudit) { close(s.report) }

func TestFinalSealLockWaitBelongsToTheTurnContext(t *testing.T) {
	store := trustedstate.Open(t.TempDir(), nil)
	if err := os.MkdirAll(filepath.Join(store.Root(), "streams", "ws"), 0o700); err != nil {
		t.Fatal(err)
	}
	unlock, err := filelock.Acquire(t.Context(), filepath.Join(store.Root(), "streams", "ws", "LOCK"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	sink := &finalizationSink{report: make(chan struct{})}
	a := New(&finalizationProvider{}, tool.NewRegistry(), sessionstore.NewSession(""), Options{EvidenceSeal: &EvidenceSeal{Store: store, Stream: "ws"}}, sink)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, "Answer the question.") }()
	select {
	case <-sink.report:
	case <-time.After(5 * time.Second):
		t.Fatal("completion report was not reached")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelling the optional seal failed a completed answer: %v", err)
		}
	case <-time.After(time.Second):
		unlock()
		<-done
		t.Fatal("completed answer still waited on the evidence stream lock after cancellation")
	}
}
