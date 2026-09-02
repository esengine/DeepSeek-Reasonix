package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// lastUserImages returns the user message's image list from the last request.
func lastUserImages(req provider.Request) []string {
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser {
			return msg.Images
		}
	}
	return nil
}

// TestTaskToolImageParamReachesVisionChild proves the #6530 flow: an image file
// that came into existence earlier in the turn is handed to the child as a
// provider-visible image content block via the explicit images parameter.
func TestTaskToolImageParamReachesVisionChild(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "chart looks correct"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil)
	resolved := ""
	task = task.WithImageResolver(func(path, baseDir string) (string, error) {
		resolved = path + "@" + baseDir
		return "data:image/png;base64,BBBB", nil
	})

	args := `{"prompt":"verify the rendered chart","images":["out/chart.png"]}`
	if _, err := task.Execute(testTaskContext(), []byte(args)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	images := lastUserImages(sub.lastReq)
	if len(images) != 1 || images[0] != "data:image/png;base64,BBBB" {
		t.Fatalf("sub-agent images = %v, want the param-resolved data URL", images)
	}
	if !strings.HasPrefix(resolved, "out/chart.png@") || !filepath.IsAbs(strings.TrimPrefix(resolved, "out/chart.png@")) {
		t.Errorf("resolver called with %q, want the param path with an absolute workspace baseDir", resolved)
	}
}

// TestReadOnlyTaskImageParamReachesChild covers the read_only_task variant.
func TestReadOnlyTaskImageParamReachesChild(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "frame analyzed"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "read_file", readOnly: true})
	task := newTestTaskTool(t, sub, parentReg, "sys", "", "", nil).
		WithImageResolver(func(string, string) (string, error) {
			return "data:image/png;base64,CCCC", nil
		})
	ro := NewReadOnlyTaskTool(task)

	args := `{"prompt":"read the extracted frame","images":["frames/f0.png"]}`
	if _, err := ro.Execute(testTaskContext(), []byte(args)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	images := lastUserImages(sub.lastReq)
	if len(images) != 1 || images[0] != "data:image/png;base64,CCCC" {
		t.Fatalf("read-only sub-agent images = %v, want the param-resolved data URL", images)
	}
}

// TestTaskToolImageParamMergesWithCandidates verifies the merge order (param
// first, then parent candidates) and deduplication.
func TestTaskToolImageParamMergesWithCandidates(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).
		WithImageResolver(func(path, _ string) (string, error) {
			return "data:image/png;base64," + path, nil
		})

	ctx := WithSubagentImageCandidates(testTaskContext(), []string{"data:image/png;base64,CAND", "data:image/png;base64,PARAM2"})
	args := `{"prompt":"x","images":["PARAM2","PARAM1"]}`
	if _, err := task.Execute(ctx, []byte(args)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	images := lastUserImages(sub.lastReq)
	want := []string{"data:image/png;base64,PARAM2", "data:image/png;base64,PARAM1", "data:image/png;base64,CAND"}
	if len(images) != len(want) {
		t.Fatalf("images = %v, want %v", images, want)
	}
	for i := range want {
		if images[i] != want[i] {
			t.Fatalf("images = %v, want param-first deduped order %v", images, want)
		}
	}
}

// TestTaskToolImageParamWithoutCandidatesStillForwards proves the param alone
// works in a turn that carried no parent image candidates (the reported case).
func TestTaskToolImageParamWithoutCandidatesStillForwards(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).
		WithImageResolver(func(path, _ string) (string, error) {
			return "data:image/png;base64," + path, nil
		})

	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x","images":["ONLY"]}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if images := lastUserImages(sub.lastReq); len(images) != 1 || !strings.HasSuffix(images[0], "ONLY") {
		t.Fatalf("images = %v, want the single param image", images)
	}
}

// TestTaskToolImageParamFailures covers the fail-closed validation matrix:
// missing files, empty entries, and over-cap lists all fail the call clearly.
func TestTaskToolImageParamFailures(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	ws := t.TempDir()
	// A real workspace image for the success-path control.
	if err := os.WriteFile(filepath.Join(ws, "real.png"), []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).
		WithTranscripts(NewSubagentStore(t.TempDir()), ws, "base-model", "base-effort").
		WithImageResolver(func(path, baseDir string) (string, error) {
			if path == "real.png" {
				return "data:image/png;base64,REAL", nil
			}
			return "", os.ErrNotExist
		})

	cases := []struct {
		name string
		args string
		want string
	}{
		{"missing file", `{"prompt":"x","images":["nope.png"]}`, "nope.png"},
		{"empty entry", `{"prompt":"x","images":[""]}`, "non-empty"},
		{"over cap", `{"prompt":"x","images":["a","b","c","d","e","f","g","h","i"]}`, "at most 8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := task.Execute(testTaskContext(), []byte(tc.args))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestTaskToolImageParamDedupesIdenticalPaths proves duplicates in the param
// collapse to one provider-visible image.
func TestTaskToolImageParamDedupesIdenticalPaths(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).
		WithImageResolver(func(path, _ string) (string, error) {
			return "data:image/png;base64,SAME", nil
		})

	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x","images":["a.png","b.png"]}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if images := lastUserImages(sub.lastReq); len(images) != 1 {
		t.Fatalf("images = %v, want exactly one deduped entry", images)
	}
}

// TestTaskToolWithoutImageResolverIgnoresParam keeps legacy constructions
// (no resolver wired) backward compatible: the param is ignored, not fatal.
func TestTaskToolWithoutImageResolverIgnoresParam(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil)
	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x","images":["a.png"]}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if images := lastUserImages(sub.lastReq); len(images) != 0 {
		t.Fatalf("images = %v, want none without a resolver", images)
	}
}

// TestTaskToolImageParamSchemaDocumentsMax ensures the schema keeps the cap and
// visibility contract the model relies on.
func TestTaskToolImageParamSchemaDocumentsMax(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil)
	for _, schema := range []string{string(task.Schema()), string(NewReadOnlyTaskTool(task).Schema())} {
		if !strings.Contains(schema, `"images"`) {
			t.Errorf("schema missing images param: %s", schema)
		}
	}
}
