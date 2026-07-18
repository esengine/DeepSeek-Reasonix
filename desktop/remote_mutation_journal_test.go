package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/remote/protocol"
)

func TestRemoteMutationJournalReusesUnknownRequestOnlyWithinEpoch(t *testing.T) {
	journal := newRemoteMutationJournal()
	next := 0
	newID := func() (protocol.RequestID, error) {
		next++
		return protocol.RequestID(fmt.Sprintf("request_%d", next)), nil
	}
	target := protocol.RuntimeTarget{WorkspaceID: "workspace_a", SessionID: "session_a"}
	semantic := struct{ Input string }{Input: "same explicit action"}

	first, err := journal.begin(newID, protocol.MethodSessionSubmit, "host_a", target, "runtime_a", semantic)
	if err != nil {
		t.Fatal(err)
	}
	if got := first.finish(context.DeadlineExceeded); got == nil {
		t.Fatal("unknown outcome was not reported")
	} else {
		var unknown *RemoteMutationOutcomeUnknownError
		if !errors.As(got, &unknown) || unknown.RequestID != "request_1" || !errors.Is(got, context.DeadlineExceeded) {
			t.Fatalf("unknown outcome = %#v, want request_1/deadline", got)
		}
	}

	retry, err := journal.begin(newID, protocol.MethodSessionSubmit, "host_a", target, "runtime_a", semantic)
	if err != nil {
		t.Fatal(err)
	}
	if retry.id() != first.id() || next != 1 {
		t.Fatalf("same-epoch explicit retry id=%q generated=%d, want %q/1", retry.id(), next, first.id())
	}
	if err := retry.finish(nil); err != nil {
		t.Fatal(err)
	}

	newSemanticAction, err := journal.begin(newID, protocol.MethodSessionSubmit, "host_a", target, "runtime_a", semantic)
	if err != nil {
		t.Fatal(err)
	}
	if newSemanticAction.id() != "request_2" {
		t.Fatalf("known completion retained request id: %q", newSemanticAction.id())
	}
	_ = newSemanticAction.finish(context.Canceled)

	changedRuntime, err := journal.begin(newID, protocol.MethodSessionSubmit, "host_a", target, "runtime_b", semantic)
	if err != nil {
		t.Fatal(err)
	}
	if changedRuntime.id() != "request_3" {
		t.Fatalf("runtime epoch change reused request id: %q", changedRuntime.id())
	}
	_ = changedRuntime.finish(context.Canceled)

	changedHost, err := journal.begin(newID, protocol.MethodSessionSubmit, "host_b", target, "runtime_b", semantic)
	if err != nil {
		t.Fatal(err)
	}
	if changedHost.id() != "request_4" {
		t.Fatalf("host epoch change reused request id: %q", changedHost.id())
	}
	_ = changedHost.finish(nil)
}

func TestRemoteMutationJournalKnownErrorAndInFlightSemantics(t *testing.T) {
	journal := newRemoteMutationJournal()
	next := 0
	newID := func() (protocol.RequestID, error) {
		next++
		return protocol.RequestID(fmt.Sprintf("request_%d", next)), nil
	}
	semantic := map[string]string{"promptId": "prompt_a"}
	first, err := journal.begin(newID, protocol.MethodPromptApprove, "host_a", protocol.RuntimeTarget{
		WorkspaceID: "workspace_a", SessionID: "session_a",
	}, "runtime_a", semantic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.begin(newID, protocol.MethodPromptApprove, "host_a", protocol.RuntimeTarget{
		WorkspaceID: "workspace_a", SessionID: "session_a",
	}, "runtime_a", semantic); !errors.Is(err, ErrRemoteMutationInFlight) {
		t.Fatalf("concurrent identical mutation error = %v", err)
	}
	known := &protocol.RemoteError{Code: protocol.ErrPromptNotPending, Message: "known response"}
	if got := first.finish(known); !errors.Is(got, known) {
		t.Fatalf("known Remote error = %v", got)
	}
	second, err := journal.begin(newID, protocol.MethodPromptApprove, "host_a", protocol.RuntimeTarget{
		WorkspaceID: "workspace_a", SessionID: "session_a",
	}, "runtime_a", semantic)
	if err != nil {
		t.Fatal(err)
	}
	if second.id() != "request_2" {
		t.Fatalf("known response retained request id: %q", second.id())
	}
	_ = second.finish(nil)
}
