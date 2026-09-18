package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/tool"
)

func TestAttachmentEndpointsDeliverFrozenBytes(t *testing.T) {
	for _, endpoint := range []string{"http", "edit", "run", "queue", "prepared"} {
		t.Run(endpoint, func(t *testing.T) {
			root := t.TempDir()
			writeVisionTestConfig(t, root)
			p := &reviewImageProvider{requests: make(chan provider.Request, 4)}
			ag := agent.New(p, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
			native := true
			c := newOwnedTestController(t, Options{WorkspaceRoot: root, SessionPath: filepath.Join(root, "session.jsonl"), Runner: ag, Executor: ag, ModelRef: "custom/vision-pro", FrozenImageInput: &native})
			path, err := SaveImageDataURLInRoot(root, "data:image/png;base64,"+tinyPNG)
			if err != nil {
				t.Fatal(err)
			}
			input := "inspect @" + path
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			switch endpoint {
			case "http":
				_, err = c.SubmitIdentifiedContext(ctx, SubmissionRequest{Input: input, HTTP: true})
			case "edit":
				_, err = c.SubmitIdentifiedContext(ctx, SubmissionRequest{Input: input, Original: "old question"})
			case "run":
				err = c.RunTurn(ctx, input)
			case "queue":
				var receipt sessioninbox.InboxReceipt
				receipt, err = c.EnqueueInbox(InboxRequest{Submit: input})
				if err == nil {
					err = os.Remove(filepath.Join(root, path))
				}
				if err == nil {
					err = c.RunInboxTurn(ctx, receipt.ItemID)
				}
			case "prepared":
				var prepared *PreparedSubmission
				prepared, err = c.PrepareSubmission(ctx, SubmissionRequest{Input: input})
				if err == nil {
					err = os.Remove(filepath.Join(root, path))
				}
				if err == nil {
					_, err = c.SubmitPreparedWithSetup(ctx, prepared, nil)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case request := <-p.requests:
				var images []string
				for _, message := range request.Messages {
					if message.Role == provider.RoleUser {
						images = append(images, message.Images...)
					}
				}
				if len(images) != 1 || images[0] != "data:image/png;base64,"+tinyPNG {
					t.Fatalf("provider did not receive exact admitted bytes: %q", images)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestAttachmentQueueEditPreservesStructuredAndAdditionalPath(t *testing.T) {
	root := t.TempDir()
	c := newOwnedTestController(t, Options{WorkspaceRoot: root})
	draft, err := c.StageImage(t.Context(), "first.png", "image/png", "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	env := sessioninbox.PromptEnvelope{AttachmentIdentities: []string{"first"}}
	if err := c.freezeInboxEnvelopeReferences(t.Context(), &env, "inspect", nil, SubmissionAttachment{ClientAttachmentID: "first", DraftID: draft.ID}); err != nil {
		t.Fatal(err)
	}
	c.ReleaseDraftImage(draft.ID)
	path, err := SaveImageDataURLInRoot(root, "data:image/png;base64,"+tinyPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.freezeInboxEnvelopeReferences(t.Context(), &env, "inspect @"+path, nil); err != nil {
		t.Fatal(err)
	}
	if len(env.ImageInputs) != 2 {
		t.Fatalf("image count = %d", len(env.ImageInputs))
	}
}
