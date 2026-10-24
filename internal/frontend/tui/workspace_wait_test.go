package tui

import (
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

func TestWorkspaceWaitIdentityAndReplay(t *testing.T) {
	wait := func(id, code string) eventwire.Event {
		return eventwire.Event{Kind: "notice", Code: code, Text: id,
			WorkspaceLease: &eventwire.WorkspaceLease{TransactionID: id}}
	}
	a, b := wait("a", event.NoticeCodeWorkspaceLease), wait("b", event.NoticeCodeWorkspaceLease)
	end := wait("a", event.NoticeCodeWorkspaceLeaseResumed)
	tr := fold(a, b, end, end, a)
	if len(tr.Items) != 2 || tr.Items[0].Code != end.Code || tr.Items[1].Code != b.Code {
		t.Fatalf("mismatched or duplicated wait: %+v", tr.Items)
	}
	legacy := wait("", event.NoticeCodeWorkspaceLease)
	legacyEnd := wait("", event.NoticeCodeWorkspaceLeaseAbandoned)
	tr = fold(legacy, a, legacyEnd)
	if len(tr.Items) != 2 || tr.Items[0].Code != legacyEnd.Code || tr.Items[1].Code != a.Code {
		t.Fatalf("legacy close matched a transaction: %+v", tr.Items)
	}
}
