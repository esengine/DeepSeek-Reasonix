package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestTaskSchemaIncludesProfileAndWritePaths(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil)
	schema := string(task.Schema())
	for _, want := range []string{`"profile"`, `"write_paths"`} {
		if !strings.Contains(schema, want) {
			t.Fatalf("schema missing %s", want)
		}
	}
	// No dynamic profile enum.
	if strings.Contains(schema, `"enum"`) {
		t.Fatalf("profile names must not be enum'd in schema: %s", schema)
	}
}

func TestTaskWriterWithoutPathsClaimsWholeWorkspace(t *testing.T) {
	root := t.TempDir()
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(6, 3))

	spec, err := task.buildTaskSpec(context.Background(), "rewrite docs", "", "", nil, nil, 0, "", "", "", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !spec.WritePaths.WholeWorkspace || spec.WritePaths.WorkspaceRoot == "" {
		t.Fatalf("writer without write_paths must claim the workspace, got %+v", spec.WritePaths)
	}
}

func TestTaskUnknownProfileRejected(t *testing.T) {
	root := t.TempDir()
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithProfileLookup(func(string) (ProfileDefinition, bool) { return ProfileDefinition{}, false })
	_, err := task.Execute(withCallContext(context.Background(), "c", event.Discard, nil, false),
		json.RawMessage(`{"prompt":"x","profile":"nope"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("err = %v", err)
	}
}

func TestTaskProfileUsesBodyAsSystemPrompt(t *testing.T) {
	root := t.TempDir()
	var sawSystem string
	prov := &captureSystemProvider{onReq: func(sys string) { sawSystem = sys }}
	task := NewTaskTool(prov, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", DefaultTaskSystemPrompt, nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithProfileLookup(func(name string) (ProfileDefinition, bool) {
			if name != "doc-rewriter" {
				return ProfileDefinition{}, false
			}
			return ProfileDefinition{Name: name, Body: "You rewrite docs carefully."}, true
		})
	_, err := task.Execute(withCallContext(context.Background(), "c", event.Discard, nil, false),
		json.RawMessage(`{"prompt":"rewrite a.md","profile":"doc-rewriter"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawSystem, "You rewrite docs carefully.") {
		t.Fatalf("system prompt = %q, want profile body", sawSystem)
	}
	if strings.Contains(sawSystem, "concise and self-contained") {
		t.Fatalf("profile must not stack DefaultTaskSystemPrompt concise text: %q", sawSystem)
	}
}

func TestTaskToolsIntersectionCannotExpand(t *testing.T) {
	root := t.TempDir()
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithProfileLookup(func(name string) (ProfileDefinition, bool) {
			return ProfileDefinition{Name: name, Body: "body", AllowedTools: []string{"read_file"}}, true
		})
	_, err := task.Execute(withCallContext(context.Background(), "c", event.Discard, nil, false),
		json.RawMessage(`{"prompt":"x","profile":"p","tools":["write_file"]}`))
	if err == nil || !strings.Contains(err.Error(), "intersection") {
		t.Fatalf("err = %v", err)
	}
}

func TestTaskResolveProfilePrecedence(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "global-m", "global-e", nil).
		WithProfileLookup(func(name string) (ProfileDefinition, bool) {
			return ProfileDefinition{Name: name, Body: "b", Model: "front-m", Effort: "front-e"}, true
		}).
		WithProfileConfigResolvers(
			func(string) string { return "cfg-m" },
			func(string) string { return "cfg-e" },
		)
	pr := task.ResolveProfile(json.RawMessage(`{"profile":"p","model":"call-m","effort":"call-e"}`))
	if pr == nil || pr.Model != "cfg-m" || pr.Effort != "cfg-e" {
		t.Fatalf("profile = %+v", pr)
	}
}

// captureSystemProvider records the system prompt of the first request.
type captureSystemProvider struct {
	onReq func(system string)
}

func (p *captureSystemProvider) Name() string { return "capture-sys" }

func (p *captureSystemProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.onReq != nil {
		for _, m := range req.Messages {
			if m.Role == provider.RoleSystem {
				p.onReq(m.Content)
				break
			}
		}
	}
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	close(ch)
	return ch, nil
}
