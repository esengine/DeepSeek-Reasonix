package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// fakeImageTool implements tool.ImageTool: text and images travel on separate
// channels, like an MCP remote tool returning a screenshot.
type fakeImageTool struct {
	text   string
	images []string
}

func (f *fakeImageTool) Name() string            { return "shot" }
func (f *fakeImageTool) Description() string     { return "returns a screenshot" }
func (f *fakeImageTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *fakeImageTool) ReadOnly() bool          { return true }
func (f *fakeImageTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	text, _, err := f.ExecuteWithImages(ctx, args)
	return text, err
}
func (f *fakeImageTool) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	return f.text, f.images, nil
}

// Tool-result images must reach the session message intact however the text
// alongside them is bounded: a head+tail splice would corrupt a base64 payload,
// so images ride outside the text rather than inside it.
func TestToolResultImagesBypassTruncation(t *testing.T) {
	dataURL := "data:image/png;base64," + strings.Repeat("QUFB", 20000) // ~80KB payload, alone over the text budget
	longText := strings.Repeat("x", MaxToolOutputBytes+1024) + "[image: image/png]"
	reg := tool.NewRegistry()
	reg.Add(&fakeImageTool{text: longText, images: []string{dataURL}})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "shot", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{ArchiveDir: testenv.TempDir(t)}, event.Discard)
	if err := a.Run(context.Background(), "take a screenshot"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var msg *provider.Message
	for i := range a.sess.conversation.Messages {
		if a.sess.conversation.Messages[i].Role == provider.RoleTool && a.sess.conversation.Messages[i].Name == "shot" {
			msg = &a.sess.conversation.Messages[i]
			break
		}
	}
	if msg == nil {
		t.Fatal("no tool message recorded for shot")
	}
	if len(msg.Images) != 1 || msg.Images[0] != dataURL {
		t.Fatalf("tool message images corrupted or missing: got %d images", len(msg.Images))
	}
	if len(msg.Content) >= len(longText) {
		t.Fatalf("tool text should have been bounded, len=%d", len(msg.Content))
	}
	if strings.Contains(msg.Content, dataURL) {
		t.Fatal("image payload must not be embedded in the tool text")
	}
}

type failingImageTool struct{ fakeImageTool }

func (f *failingImageTool) ExecuteWithImages(context.Context, json.RawMessage) (string, []string, error) {
	return f.text, f.images, errors.New("element not found")
}

// A failed call's screenshot is the state that explains the failure, so the
// error result carries it like a success would.
func TestFailedToolResultKeepsItsImages(t *testing.T) {
	dataURL := "data:image/png;base64,QUFB"
	reg := tool.NewRegistry()
	reg.Add(&failingImageTool{fakeImageTool{text: "[image: image/png]", images: []string{dataURL}}})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "shot", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{ArchiveDir: testenv.TempDir(t)}, event.Discard)
	if err := a.Run(context.Background(), "click the button"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, m := range prov.requests[len(prov.requests)-1].Messages {
		if m.Role == provider.RoleTool && m.Name == "shot" {
			if !strings.Contains(m.Content, "element not found") {
				t.Fatalf("the failure did not reach the model: %q", m.Content)
			}
			if len(m.Images) != 1 || m.Images[0] != dataURL {
				t.Fatalf("failed result images = %v, want the screenshot", m.Images)
			}
			return
		}
	}
	t.Fatal("no tool result for shot reached the provider")
}

// The window is where a person watches the agent work on a page or an
// application they cannot see for themselves. The result text names the
// picture; the card can only show it if the event carries it.
func TestToolResultEventCarriesWhatTheCallShowed(t *testing.T) {
	dataURL := "data:image/png;base64,QUFB"
	reg := tool.NewRegistry()
	reg.Add(&fakeImageTool{text: "front window\n[image: screenshot]", images: []string{dataURL}})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "shot", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	sink := &recordSink{}
	a := New(prov, reg, sessionstore.NewSession(""), Options{ArchiveDir: testenv.TempDir(t)}, sink)
	if err := a.Run(context.Background(), "take a screenshot"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	results := sink.kinds(event.ToolResult)
	if len(results) != 1 {
		t.Fatalf("the window was sent %d tool results, want 1", len(results))
	}
	if got := results[0].Tool.Images; len(got) != 1 || got[0] != dataURL {
		t.Fatalf("the tool result carried %d images (%v), want the one the call showed", len(got), got)
	}
}
