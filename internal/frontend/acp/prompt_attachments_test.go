package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"reasonix/internal/contract/event"
)

const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="

var attachmentRef = regexp.MustCompile(`@(\.reasonix/attachments/\S+)`)

// promptInput sends one prompt and returns what the controller's runner was
// handed, plus the prompt's RPC outcome.
func promptInput(t *testing.T, cwd string, blocks []ContentBlock) (string, frame) {
	t.Helper()
	inputs := make(chan string, 1)
	factory := &fakeFactory{behavior: func(_ context.Context, _ event.Sink, input string) error {
		inputs <- input
		return nil
	}}
	client, stop := startServer(t, factory)
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	var nr SessionNewResult
	if err := json.Unmarshal(client.call(t, "session/new", SessionNewParams{Cwd: cwd}).Result, &nr); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	_, resp := drainPrompt(t, client, client.callAsync("session/prompt", SessionPromptParams{SessionID: nr.SessionID, Prompt: blocks}))
	select {
	case input := <-inputs:
		return input, resp
	default:
		return "", resp
	}
}

func assertAttachedImage(t *testing.T, cwd, input string) {
	t.Helper()
	m := attachmentRef.FindStringSubmatch(input)
	if m == nil {
		t.Fatalf("turn input = %q, want an @.reasonix/attachments/ reference to the image", input)
	}
	raw, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(m[1])))
	if err != nil {
		t.Fatalf("referenced attachment: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("referenced attachment is empty")
	}
}

func TestPromptImageBlockReachesTheTurnAsAnAttachment(t *testing.T) {
	cwd := t.TempDir()
	input, resp := promptInput(t, cwd, []ContentBlock{
		{Type: "text", Text: "what is in this picture?"},
		{Type: "image", MimeType: "image/png", Data: onePixelPNG},
	})
	if resp.Error != nil {
		t.Fatalf("prompt error = %+v", resp.Error)
	}
	assertAttachedImage(t, cwd, input)
}

func TestPromptEmbeddedBlobResourceReachesTheTurnAsAnAttachment(t *testing.T) {
	cwd := t.TempDir()
	input, resp := promptInput(t, cwd, []ContentBlock{
		{Type: "text", Text: "what is in this picture?"},
		{Type: "resource", Resource: &ResourceContents{URI: "file:///uploads/image.png", MimeType: "image/png", Blob: onePixelPNG}},
	})
	if resp.Error != nil {
		t.Fatalf("prompt error = %+v", resp.Error)
	}
	assertAttachedImage(t, cwd, input)
}

func TestPromptImageOnlyIsATurn(t *testing.T) {
	cwd := t.TempDir()
	input, resp := promptInput(t, cwd, []ContentBlock{{Type: "image", MimeType: "image/png", Data: onePixelPNG}})
	if resp.Error != nil {
		t.Fatalf("prompt error = %+v", resp.Error)
	}
	assertAttachedImage(t, cwd, input)
}

func TestPromptUndecodableImageIsRefusedNotDropped(t *testing.T) {
	_, resp := promptInput(t, t.TempDir(), []ContentBlock{
		{Type: "text", Text: "look"},
		{Type: "image", MimeType: "image/png", Data: "not base64!"},
	})
	if resp.Error == nil || resp.Error.Code != ErrInvalidParams {
		t.Fatalf("prompt error = %+v, want invalid params", resp.Error)
	}
}
