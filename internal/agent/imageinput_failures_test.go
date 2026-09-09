package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type emptyImageProvider struct{}

func (emptyImageProvider) Name() string { return "empty" }
func (emptyImageProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

func TestImageFailureKeepsEachBatchResult(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, errors.New("network unavailable"), nil} {
		cfg := &imageinput.Config{Model: "vision/model", Resolve: func(string) (provider.Provider, error) { return emptyImageProvider{}, failure }}
		reg := tool.NewRegistry()
		shot := &detailedImageTool{fakeImageTool: fakeImageTool{text: "saved", images: []string{"data:image/png;base64,QUFB"}}}
		reg.Add(shot)
		p := &scriptedProvider{name: "text", turns: [][]provider.Chunk{{toolCallChunk("a", "shot", `{}`), toolCallChunk("b", "shot", `{}`), {Type: provider.ChunkDone}}, {{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}}}
		a := New(p, reg, NewSession("sys"), Options{ImageInput: cfg, ModelRef: "text/model"}, event.Discard)
		if err := a.Run(context.Background(), "inspect"); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, m := range a.Session().Snapshot() {
			if m.Role == provider.RoleTool {
				ids = append(ids, m.ToolCallID)
				text := m.Content
				if m.RawContent != "" {
					text = m.RawContent
				}
				if m.ToolRunState != provider.ToolRunCompleted || !strings.Contains(text, "saved") || !strings.Contains(text, "unavailable") || len(m.Images) != 1 {
					t.Fatalf("result: %+v", m)
				}
			}
		}
		if strings.Join(ids, ",") != "a,b" || shot.calls.Load() != 2 {
			t.Fatalf("ids=%v calls=%d", ids, shot.calls.Load())
		}
	}
}

func TestRejectedToolImagesNeverInvokeVision(t *testing.T) {
	vp := &summaryProvider{}
	cfg := &imageinput.Config{Model: "vision/model", Resolve: func(string) (provider.Provider, error) { return vp, nil }}
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolAfter {
			return blockWith("withheld"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	reg := tool.NewRegistry()
	reg.Add(&fakeImageTool{text: "saved", images: []string{"data:image/png;base64,QUFB"}})
	a := New(nil, reg, NewSession("sys"), Options{ImageInput: cfg, Extensions: newExtDispatcher(client, true, nil, extension.PointToolAfter)}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "shot", Arguments: `{}`})
	if out.errMsg == "" || vp.calls.Load() != 0 {
		t.Fatalf("error=%q calls=%d", out.errMsg, vp.calls.Load())
	}
}
