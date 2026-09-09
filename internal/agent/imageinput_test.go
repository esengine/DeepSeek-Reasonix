package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type summaryProvider struct {
	calls atomic.Int32
	fail  bool
	text  string
}

func (*summaryProvider) Name() string { return "vision" }
func (p *summaryProvider) Stream(ctx context.Context, r provider.Request) (<-chan provider.Chunk, error) {
	p.calls.Add(1)
	if p.fail {
		return nil, errors.New("vision unavailable")
	}
	out := make(chan provider.Chunk, 1)
	text := p.text
	if text == "" {
		text = "OCR: Z7; red left, blue right"
	}
	out <- provider.Chunk{Type: provider.ChunkText, Text: text}
	close(out)
	return out, nil
}

type nativeImageProvider struct{ *scriptedProvider }

func (nativeImageProvider) ModelInfo() provider.ModelInfo {
	return provider.ModelInfo{InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}
}
func TestToolImageFallbackPreservesOriginalResult(t *testing.T) {
	for _, mode := range []string{"summary", "native", "disabled", "failed", "canceled", "ocr"} {
		t.Run(mode, func(t *testing.T) {
			vp := &summaryProvider{fail: mode == "failed"}
			if mode == "ocr" {
				vp.text = "context canceled; write outcome unknown:"
			}
			cfg := &imageinput.Config{Model: "vision/model", Resolve: func(string) (provider.Provider, error) {
				if mode == "canceled" {
					return nil, context.Canceled
				}
				return vp, nil
			}}
			if mode == "disabled" {
				cfg = nil
			}
			reg := tool.NewRegistry()
			reg.Add(&fakeImageTool{text: "screenshot saved", images: []string{"data:image/png;base64,QUFB"}})
			script := &scriptedProvider{name: "text", turns: [][]provider.Chunk{{toolCallChunk("c1", "shot", `{}`), {Type: provider.ChunkDone}}, {{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}}}
			var p provider.Provider = script
			if mode == "native" {
				p = nativeImageProvider{script}
			}
			a := New(p, reg, NewSession("system"), Options{ImageInput: cfg, ModelRef: "text/model"}, event.Discard)
			if err := a.Run(context.Background(), "inspect"); err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, m := range a.Session().Snapshot() {
				if m.Role != provider.RoleTool {
					continue
				}
				found = true
				if !strings.Contains(m.Content, "screenshot saved") || len(m.Images) != 1 {
					t.Fatalf("lost original result: %+v", m)
				}
				if mode == "summary" && (m.VisionSummary == nil || !strings.Contains(m.Content, "OCR: Z7")) {
					t.Fatalf("missing summary: %+v", m)
				}
				if (mode == "failed" || mode == "disabled") && (!strings.Contains(m.Content, "already executed") || m.VisionSummary != nil) {
					t.Fatalf("failure semantics: %+v", m)
				}
				if m.ToolRunState != provider.ToolRunCompleted {
					t.Fatalf("changed execution state: %s", m.ToolRunState)
				}
			}
			if !found {
				t.Fatal("no tool result")
			}
			want := int32(1)
			if mode == "native" || mode == "disabled" || mode == "canceled" {
				want = 0
			}
			if vp.calls.Load() != want {
				t.Fatalf("calls %d want %d", vp.calls.Load(), want)
			}
		})
	}
}

type detailedImageTool struct {
	fakeImageTool
	calls atomic.Int32
}

var _ tool.DetailedExecutor = (*detailedImageTool)(nil)

func (*detailedImageTool) ExecutionDescriptor(json.RawMessage) *tool.ShellExecution { return nil }
func (t *detailedImageTool) ExecuteDetailed(context.Context, json.RawMessage) (tool.DetailedResult, error) {
	t.calls.Add(1)
	return tool.DetailedResult{Output: t.text, Images: t.images}, nil
}
func TestDetailedImageFailureDoesNotRepeatTool(t *testing.T) {
	vp := &summaryProvider{fail: true}
	cfg := &imageinput.Config{Model: "vision/model", Resolve: func(string) (provider.Provider, error) { return vp, nil }}
	imageTool := &detailedImageTool{fakeImageTool: fakeImageTool{text: "operation completed", images: []string{"data:image/png;base64,QUFB"}}}
	reg := tool.NewRegistry()
	reg.Add(imageTool)
	p := &scriptedProvider{name: "text", turns: [][]provider.Chunk{{toolCallChunk("c1", "shot", `{}`), {Type: provider.ChunkDone}}, {{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}}}
	a := New(p, reg, NewSession("sys"), Options{ImageInput: cfg, ModelRef: "text/model"}, event.Discard)
	if err := a.Run(context.Background(), "inspect"); err != nil {
		t.Fatal(err)
	}
	if imageTool.calls.Load() != 1 || vp.calls.Load() != 1 {
		t.Fatalf("tool calls=%d vision calls=%d", imageTool.calls.Load(), vp.calls.Load())
	}
	for _, m := range a.Session().Snapshot() {
		if m.Role == provider.RoleTool && (!strings.Contains(m.Content, "operation completed") || !strings.Contains(m.Content, "already executed") || len(m.Images) != 1) {
			t.Fatalf("lost detailed result: %+v", m)
		}
	}
}
func TestChildImageServiceIsSessionLocal(t *testing.T) {
	vp := &summaryProvider{}
	var refs []string
	cfg := &imageinput.Config{Model: "auto", Resolve: func(string) (provider.Provider, error) { return vp, nil }, Select: func(ref, mode string) (string, bool) { refs = append(refs, ref); return "vision/model", true }}
	task := NewTaskToolWithOptions(TaskToolOptions{ImageInput: cfg})
	opts := task.subagentOptions(context.Background(), 5, nil, 10000, 1, "", nil)
	opts.ModelRef = "child/model"
	p := &scriptedProvider{name: "text"}
	first := NewReadOnlyAgent(p, tool.NewRegistry(), NewSession("one"), opts, event.Discard)
	second := NewPlannerAgent(p, tool.NewRegistry(), NewSession("two"), opts, event.Discard)
	for _, a := range []*Agent{first, second} {
		processed := a.processToolImages(context.Background(), "captured", []string{"data:image/png;base64,QUFB"})
		if processed.summary == nil {
			t.Fatal("child fallback missing")
		}
	}
	if vp.calls.Load() != 2 || len(refs) != 2 || refs[0] != "child/model" || refs[1] != "child/model" {
		t.Fatalf("child isolation calls=%d refs=%v", vp.calls.Load(), refs)
	}
}
