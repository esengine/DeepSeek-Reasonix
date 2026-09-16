package transcript

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/turnevent"
)

func TestRuntimeRebindResetsIdleFollowerAndRejectsStaleFrames(t *testing.T) {
	p, initial := newFollowProjection(t)
	p.SetRuntimeEpoch("next-runtime")
	response := followChanges(t, p, FollowRequest{Subscription: initial.Subscription, AfterRevision: initial.Snapshot.ProjectionRevision})
	if !response.ResetRequired {
		t.Fatal("idle follower did not learn runtime changed")
	}
	before := p.Boundary()
	if err := p.ApplyFrame(turnevent.Envelope{SessionID: testIdentity.SessionID, RuntimeEpoch: testIdentity.RuntimeEpoch}, before.CoveredThroughSeq); err == nil {
		t.Fatal("old controller frame accepted after runtime rebind")
	}
	if p.Boundary() != before {
		t.Fatal("rejected frame changed publisher state")
	}
	if err := p.ApplyFrame(turnevent.Envelope{SessionID: "another-session", RuntimeEpoch: "next-runtime"}, before.CoveredThroughSeq); err == nil {
		t.Fatal("foreign session frame accepted")
	}
}

func newFollowProjection(t *testing.T) (*Projection, FollowResponse) {
	t.Helper()
	p, err := NewProjection(testIdentity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := p.Follow(t.Context(), FollowRequest{})
	if err != nil || initial.Snapshot == nil || initial.Subscription == "" || initial.ProtocolVersion != FollowProtocolVersion {
		t.Fatalf("initial follow: response=%+v error=%v", initial, err)
	}
	return p, initial
}

func followChanges(t *testing.T, p *Projection, request FollowRequest) FollowResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	response, err := p.Follow(ctx, request)
	if err != nil {
		t.Fatalf("follow changes: %v", err)
	}
	return response
}

func TestFollowInitialCutAndConcurrentCommitHaveNoSubscriptionGap(t *testing.T) {
	// Race both legal orderings: the accepted message belongs either to the
	// initial cut or its queued suffix. It must never fall between them.
	for range 32 {
		p, err := NewProjection(testIdentity, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		start, committed := make(chan struct{}), make(chan struct{})
		go func() {
			<-start
			p.AcceptBusiness([]Message{{RecordID: "m:answer", MessageID: "answer", Role: "assistant", Content: "answer"}}, 1, "turn", false)
			close(committed)
		}()
		close(start)
		initial, err := p.Follow(t.Context(), FollowRequest{})
		if err != nil || initial.Snapshot == nil {
			t.Fatalf("initial follow: %v", err)
		}
		<-committed
		if initial.Snapshot.CoveredThroughSeq == 1 {
			if len(initial.Snapshot.Records) != 1 || initial.Snapshot.Records[0].Message.Content != "answer" {
				t.Fatal("initial cut advertises a commit without its message")
			}
		} else {
			suffix := followChanges(t, p, FollowRequest{Subscription: initial.Subscription, AfterRevision: initial.Snapshot.ProjectionRevision})
			if suffix.ResetRequired || len(suffix.Changes) != 1 || suffix.Changes[0].CommitSeq != 1 || len(suffix.Changes[0].Records) != 1 || suffix.Changes[0].Records[0].Content != "answer" {
				t.Fatalf("message lost between registration and initial cut: %+v", suffix)
			}
		}
	}
}

func TestFollowRetainsCommitsBeforeNextPollAndRetriesUnacknowledgedSuffix(t *testing.T) {
	p, initial := newFollowProjection(t)
	p.AcceptBusiness([]Message{{RecordID: "m:answer", MessageID: "answer", Role: "assistant", Content: "answer"}}, 1, "turn", false)
	request := FollowRequest{Subscription: initial.Subscription, AfterRevision: initial.Snapshot.ProjectionRevision}
	first := followChanges(t, p, request)
	retry := followChanges(t, p, request)
	if len(first.Changes) != 1 || !reflect.DeepEqual(first, retry) {
		t.Fatalf("retry altered unacknowledged suffix: first=%+v retry=%+v", first, retry)
	}
	p.AcceptBusiness(nil, 2, "turn", false)
	request.AfterRevision = first.Changes[0].Revision
	next := followChanges(t, p, request)
	if len(next.Changes) != 1 || next.Changes[0].FirstSeq != 2 || next.Changes[0].CommitSeq != 2 {
		t.Fatalf("acknowledgment repeated or omitted a commit: %+v", next)
	}
}

func TestFollowKeepsBusinessCoverageSeparateFromDisplayRevision(t *testing.T) {
	p, initial := newFollowProjection(t)
	p.AcceptBusiness(nil, 4, "turn", false)
	p.SetDurableSequence(4)
	businessFrame(t, p, 4, event.Event{Kind: event.Text, MessageID: "answer", Text: "first "})
	businessFrame(t, p, 4, event.Event{Kind: event.Text, MessageID: "answer", Text: "second"})
	suffix := followChanges(t, p, FollowRequest{Subscription: initial.Subscription, AfterRevision: initial.Snapshot.ProjectionRevision})
	if len(suffix.Changes) != 4 {
		t.Fatalf("expected business batch, durability update and two frames, got %+v", suffix)
	}
	for index, change := range suffix.Changes {
		if change.CommitSeq != 4 || change.Revision <= initial.Snapshot.ProjectionRevision || index > 0 && change.Revision <= suffix.Changes[index-1].Revision {
			t.Fatalf("invalid coverage or revision: %+v", suffix.Changes)
		}
		if index == 0 {
			if change.FirstSeq != 1 || change.Event != nil {
				t.Fatalf("non-visible business batch lost its coverage range: %+v", change)
			}
		} else if index == 1 {
			if change.FirstSeq != 0 || change.Event != nil || len(change.Records) != 0 || change.DurableSeq != 4 {
				t.Fatalf("durability update manufactured business or frame data: %+v", change)
			}
		} else if change.FirstSeq != 0 || change.Event == nil || change.DurableSeq != 4 {
			t.Fatalf("frame manufactured business events or omitted durability: %+v", change)
		}
	}
}

func TestFollowDurabilityAdvancesSnapshotRevisionWithoutMutatingCachedCut(t *testing.T) {
	p, err := NewProjection(testIdentity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	p.AcceptBusiness([]Message{{RecordID: "m:answer", MessageID: "answer", Role: "assistant", Content: "accepted answer"}}, 4, "turn", false)
	accepted := snapshot(t, p)
	if accepted.DurableSeq != 0 || accepted.CoveredThroughSeq != 4 {
		t.Fatalf("unexpected accepted-only cut: %+v", accepted.Boundary)
	}
	p.SetDurableSequence(4)
	durable := snapshot(t, p)
	if durable.SnapshotID == accepted.SnapshotID || durable.ProjectionRevision <= accepted.ProjectionRevision || durable.DurableSeq != 4 || durable.CoveredThroughSeq != 4 {
		t.Fatalf("durability change reused a stale snapshot cut: accepted=%+v durable=%+v", accepted.Boundary, durable.Boundary)
	}
	if !reflect.DeepEqual(durable.Records, accepted.Records) {
		t.Fatal("durability-only change modified visible records")
	}
	cached, err := p.Snapshot(PageRequest{SnapshotID: accepted.SnapshotID, Before: accepted.TotalRecords})
	if err != nil || cached.Stale || cached.DurableSeq != 0 || cached.ProjectionRevision != accepted.ProjectionRevision || cached.CoveredThroughSeq != 4 {
		t.Fatalf("cached accepted cut was relabeled with newer durability: %+v error=%v", cached.Boundary, err)
	}
	p.SetDurableSequence(4)
	p.SetDurableSequence(2)
	unchanged := snapshot(t, p)
	if unchanged.Boundary != durable.Boundary {
		t.Fatalf("duplicate or stale durability receipt changed the cut: before=%+v after=%+v", durable.Boundary, unchanged.Boundary)
	}
}

func TestFollowOverflowRequiresNewBaseline(t *testing.T) {
	for _, mode := range []string{"count", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			p, initial := newFollowProjection(t)
			var finalSequence uint64
			if mode == "count" {
				for sequence := uint64(1); sequence <= 257; sequence++ {
					p.AcceptBusiness(nil, sequence, "turn", false)
					finalSequence = sequence
				}
			} else {
				p.AcceptBusiness([]Message{{RecordID: "m:large", MessageID: "large", Role: "assistant", Content: strings.Repeat("x", MaxResponseBytes/2)}}, 1, "turn", false)
				finalSequence = 1
			}
			response := followChanges(t, p, FollowRequest{Subscription: initial.Subscription, AfterRevision: initial.Snapshot.ProjectionRevision})
			if !response.ResetRequired || len(response.Changes) != 0 {
				t.Fatalf("overflow silently returned a partial suffix: %+v", response)
			}
			// Acknowledging an arbitrarily newer revision must not clear the
			// reset marker and make an incomplete subscription look healthy.
			retry := followChanges(t, p, FollowRequest{Subscription: initial.Subscription, AfterRevision: ^uint64(0)})
			if !retry.ResetRequired {
				t.Fatal("overflow reset was cleared without a new baseline")
			}
			replacement, err := p.Follow(t.Context(), FollowRequest{})
			if err != nil || replacement.Snapshot == nil || replacement.Snapshot.CoveredThroughSeq != finalSequence {
				t.Fatalf("replacement baseline lost committed data: %+v error=%v", replacement, err)
			}
		})
	}
}

// Done is evaluated by the long-poll select after it has released the owner
// lock. This provides a deterministic barrier without polling or sleeping.
type followWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *followWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestFollowLongPollReleasesOwnerAndHonorsCancellation(t *testing.T) {
	p, initial := newFollowProjection(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiting := &followWaitingContext{Context: ctx, waiting: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		_, err := p.Follow(waiting, FollowRequest{Subscription: initial.Subscription, AfterRevision: initial.Snapshot.ProjectionRevision})
		result <- err
	}()
	<-waiting.waiting
	// Snapshot needs the same publisher lock as acceptance; it must remain
	// available while a client is waiting on the network-facing operation.
	if _, err := p.Snapshot(PageRequest{}); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("long poll cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("long poll ignored cancellation")
	}
}

func TestFollowCloseReleasesSubscriptionAndPendingLongPoll(t *testing.T) {
	p, initial := newFollowProjection(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waiting := &followWaitingContext{Context: ctx, waiting: make(chan struct{})}
	result := make(chan FollowResponse, 1)
	go func() {
		response, _ := p.Follow(waiting, FollowRequest{Subscription: initial.Subscription, AfterRevision: initial.Snapshot.ProjectionRevision})
		result <- response
	}()
	<-waiting.waiting
	if _, err := p.Follow(t.Context(), FollowRequest{Subscription: initial.Subscription, Close: true}); err != nil {
		t.Fatal(err)
	}
	missing := followChanges(t, p, FollowRequest{Subscription: initial.Subscription})
	if !missing.ResetRequired {
		t.Fatal("closed subscription remained usable")
	}
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("closed subscription retained its pending long poll")
	}
}
