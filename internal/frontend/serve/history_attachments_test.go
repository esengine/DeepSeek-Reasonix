package serve

import (
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestHistoryNamesStoredAttachmentsFromTheHost(t *testing.T) {
	const ref = ".reasonix/attachments/clipboard-20260101-000000.000000-000001.png"
	resolve := func(text string) []event.Attachment {
		if text != "look @"+ref {
			return nil
		}
		return []event.Attachment{{Path: ref}}
	}
	got := historyMessagesWith([]provider.Message{
		{Role: provider.RoleUser, Content: "look @" + ref},
		{Role: provider.RoleUser, Content: "typed lookalike @" + ref + " but not stored"},
		{Role: provider.RoleUser, Content: "look @" + ref, HostAuthored: true},
	}, resolve)
	if len(got[0].Attachments) != 1 || got[0].Attachments[0].Path != ref {
		t.Fatalf("attachments = %+v", got[0].Attachments)
	}
	// The picture is drawn beside the words; the token that resolves it is not
	// part of what the person said, and a reopened window shows the same line.
	if got[0].Content != "look" {
		t.Fatalf("content = %q, want the words without the attachment token", got[0].Content)
	}
	if len(got[1].Attachments) != 0 {
		t.Fatalf("text the host did not resolve carried attachments: %+v", got[1].Attachments)
	}
	if len(got[2].Attachments) != 0 {
		t.Fatalf("a host-authored line carried attachments: %+v", got[2].Attachments)
	}
}
