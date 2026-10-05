package control

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

// The allow list is the user's, so no approval mode widens it: a host outside
// it waits for a person, and a session grant covers that host and no other.
func TestEgressApprovalWaitsForAPersonAndGrantsPerHost(t *testing.T) {
	requests := make(chan event.Approval, 2)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			requests <- e.Approval
		}
	})})
	c.SetAutoApproveTools(true)
	approver := sandboxEscapeApprover{c}

	type result struct {
		allow bool
		err   error
	}
	done := make(chan result, 1)
	go func() {
		allow, err := approver.ApproveEgress(context.Background(), "pypi.org")
		done <- result{allow, err}
	}()
	var approval event.Approval
	select {
	case approval = <-requests:
	case <-time.After(30 * time.Second):
		t.Fatal("egress approval was not requested")
	}
	if approval.Tool != NetworkEgressApprovalTool || approval.Subject != "pypi.org" {
		t.Fatalf("approval = %q %q", approval.Tool, approval.Subject)
	}
	select {
	case got := <-done:
		t.Fatalf("tool auto-approval answered an egress approval: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	c.Approve(approval.ID, true, true, false)
	if got := <-done; got.err != nil || !got.allow {
		t.Fatalf("approved egress = %+v", got)
	}

	if allow, err := approver.ApproveEgress(context.Background(), "pypi.org"); err != nil || !allow {
		t.Fatalf("session grant did not cover the host: %v %v", allow, err)
	}
	select {
	case a := <-requests:
		t.Fatalf("a granted host was asked again: %+v", a)
	default:
	}

	go func() {
		allow, err := approver.ApproveEgress(context.Background(), "evil.example")
		done <- result{allow, err}
	}()
	select {
	case approval = <-requests:
	case <-time.After(30 * time.Second):
		t.Fatal("a different host rode the first host's grant")
	}
	c.Approve(approval.ID, false, false, false)
	if got := <-done; got.err != nil || got.allow {
		t.Fatalf("declined egress = %+v", got)
	}
}
