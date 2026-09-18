package control

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/tool"
)

func TestMixedDraftAndOrdinaryWorkspaceImageAreBothPrepared(t *testing.T) {
	root := t.TempDir()
	c := newOwnedTestController(t, Options{WorkspaceRoot: root})
	draft, err := c.StageImage(t.Context(), "draft.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ordinary.png"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	prepared, err := c.PrepareSubmission(t.Context(), SubmissionRequest{Input: "inspect @ordinary.png", Attachments: []SubmissionAttachment{{ClientAttachmentID: "draft", DraftID: draft.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.images.inputs) != 2 {
		t.Fatalf("mixed image count = %d", len(prepared.images.inputs))
	}
}

func TestInvalidImageBatchNeverStartsEndpointOrGoal(t *testing.T) {
	for _, endpoint := range []string{"normal", "http", "edit", "goal", "queue", "run"} {
		t.Run(endpoint, func(t *testing.T) {
			root := t.TempDir()
			p := &reviewImageProvider{requests: make(chan provider.Request, 4)}
			ag := agent.New(p, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
			c := newOwnedTestController(t, Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl"), Runner: ag, Executor: ag})
			good, err := SaveImageDataURLInRoot(root, "data:image/png;base64,"+tinyPNG)
			if err != nil {
				t.Fatal(err)
			}
			input := "inspect @" + good + " @.reasonix/attachments/missing.png"
			setupCalled := false
			switch endpoint {
			case "queue":
				_, err = c.EnqueueInboxContext(t.Context(), InboxRequest{Submit: input})
			case "run":
				err = c.RunTurn(t.Context(), input)
			default:
				req := SubmissionRequest{ID: "reject-batch", Input: input, HTTP: endpoint == "http"}
				if endpoint == "edit" {
					req.Original = "original"
				}
				_, err = c.SubmitIdentifiedWithSetupContext(t.Context(), req, func() error { setupCalled = true; return nil })
			}
			if err == nil || setupCalled || c.Running() {
				t.Fatalf("partial admission: err=%v setup=%v running=%v", err, setupCalled, c.Running())
			}
			if len(p.requests) != 0 {
				t.Fatal("invalid batch reached provider")
			}
			for _, msg := range ag.Session().Snapshot() {
				if msg.Role == provider.RoleUser {
					t.Fatal("rejected batch appended a user message")
				}
			}
		})
	}
}

func TestCorruptQueuedImageBlocksExecution(t *testing.T) {
	root := t.TempDir()
	p := &reviewImageProvider{requests: make(chan provider.Request, 4)}
	ag := agent.New(p, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl"), Runner: ag, Executor: ag})
	draft, err := c.StageImage(t.Context(), "queued.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := c.EnqueueInboxContext(t.Context(), InboxRequest{Submit: "inspect", Attachments: []SubmissionAttachment{{ClientAttachmentID: "queued", DraftID: draft.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	digest := draft.Ref.Content.Digest
	path := filepath.Join(c.attachmentService().Store().Root(), "objects", digest[:2], digest[2:4], digest)
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.RunInboxTurn(t.Context(), receipt.ItemID); err == nil {
		t.Fatal("corrupt queued image executed")
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := st.ReadItem(receipt.ItemID)
	if err != nil || meta.State != sessioninbox.StateBlocked {
		t.Fatalf("queue state=%v err=%v", meta.State, err)
	}
	if len(p.requests) != 0 {
		t.Fatal("corrupt queued image reached provider")
	}
}

func TestAttachmentFingerprintIgnoresReboundCredentials(t *testing.T) {
	a := SubmissionRequest{Input: "inspect @draft:old", Display: "![photo](draft:old)", DraftIDs: []string{"old"}, Attachments: []SubmissionAttachment{{ClientAttachmentID: "photo", DraftID: "old"}}}
	b := cloneSubmissionRequest(a)
	b.Input, b.Display, b.DraftIDs, b.Attachments[0].DraftID = "inspect @draft:new", "![photo](draft:new)", []string{"new"}, "new"
	if canonicalSubmissionFingerprint(a) != canonicalSubmissionFingerprint(b) {
		t.Fatal("transport credential changed request identity")
	}
	if a.Attachments[0].DraftID != "old" {
		t.Fatal("fingerprinting mutated caller request")
	}
	b.Attachments[0].ClientAttachmentID = "different"
	if canonicalSubmissionFingerprint(a) == canonicalSubmissionFingerprint(b) {
		t.Fatal("logical attachment identity was ignored")
	}
}

func TestQueueReceiptSurvivesCredentialRenewalAndRelease(t *testing.T) {
	root := t.TempDir()
	c := newOwnedTestController(t, Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl")})
	draft, err := c.StageImage(t.Context(), "queue.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	req := InboxRequest{Submit: "inspect", Display: "![photo](draft:" + draft.ID + ")", Idempotency: "queue-stable", Attachments: []SubmissionAttachment{{ClientAttachmentID: "photo", DraftID: draft.ID}}}
	first, err := c.EnqueueInboxContext(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	c.ReleaseDraftImage(draft.ID)
	req.Attachments[0].DraftID = "replacement-credential"
	req.Display = "![photo](draft:replacement-credential)"
	again, err := c.EnqueueInboxContext(t.Context(), req)
	if err != nil || first.ItemID != again.ItemID {
		t.Fatalf("retry=%+v err=%v, want %s", again, err, first.ItemID)
	}
	req.Submit = "different user intent"
	if _, err := c.EnqueueInboxContext(t.Context(), req); err == nil {
		t.Fatal("different queue intent reused receipt")
	}
}
