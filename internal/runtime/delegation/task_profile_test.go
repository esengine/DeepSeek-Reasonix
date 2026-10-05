package delegation

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestTaskSchemaIncludesProfileAndWritePaths(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil)
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

func TestWritePathsGuidanceNamesFilesOverDirectories(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil)
	fleet := NewFleetTool(task)

	type props map[string]struct {
		Description string `json:"description"`
	}
	var taskSchema struct {
		Properties props `json:"properties"`
	}
	if err := json.Unmarshal(task.Schema(), &taskSchema); err != nil {
		t.Fatal(err)
	}
	var fleetSchema struct {
		Properties struct {
			Tasks struct {
				Items struct {
					Properties props `json:"properties"`
				} `json:"items"`
			} `json:"tasks"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(fleet.Schema(), &fleetSchema); err != nil {
		t.Fatal(err)
	}

	for name, text := range map[string]string{
		"task description":        task.Description(),
		"task write_paths":        taskSchema.Properties["write_paths"].Description,
		"fleet description":       fleet.Description(),
		"fleet items write_paths": fleetSchema.Properties.Tasks.Items.Properties["write_paths"].Description,
	} {
		for _, want := range []string{"file paths when the targets are known", "a directory overlaps every path beneath it"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not say %q: %s", name, want, text)
			}
		}
	}
}

func TestTaskWriterWithoutPathsClaimsWholeWorkspace(t *testing.T) {
	root := testenv.TempDir(t)
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(6, 3))

	spec, err := task.buildTaskSpec(context.Background(), "rewrite docs", "", "", nil, nil, 0, "", "", "", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !spec.Grant.WritePaths.WholeWorkspace || spec.Grant.WritePaths.WorkspaceRoot == "" {
		t.Fatalf("writer without write_paths must claim the workspace, got %+v", spec.Grant.WritePaths)
	}
}

func TestTaskUnknownProfileRejected(t *testing.T) {
	root := testenv.TempDir(t)
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithProfileLookup(func(string) (ProfileDefinition, bool) { return ProfileDefinition{}, false })
	_, err := task.Execute(agent.WithCallContext(context.Background(), "c", event.Discard, nil, false),
		json.RawMessage(`{"prompt":"x","profile":"nope"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("err = %v", err)
	}
}

func TestTaskProfileUsesBodyAsSystemPrompt(t *testing.T) {
	root := testenv.TempDir(t)
	var sawSystem string
	prov := &captureSystemProvider{onReq: func(sys string) { sawSystem = sys }}
	task := NewTaskTool(prov, nil, tool.NewRegistry(), 20, 0, 0, 0, 0.0, "", DefaultTaskSystemPrompt, nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithProfileLookup(func(name string) (ProfileDefinition, bool) {
			if name != "doc-rewriter" {
				return ProfileDefinition{}, false
			}
			return ProfileDefinition{Name: name, Body: "You rewrite docs carefully."}, true
		})
	_, err := task.Execute(agent.WithCallContext(context.Background(), "c", event.Discard, nil, false),
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
	root := testenv.TempDir(t)
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithProfileLookup(func(name string) (ProfileDefinition, bool) {
			return ProfileDefinition{Name: name, Body: "body", AllowedTools: []string{"read_file"}}, true
		})
	_, err := task.Execute(agent.WithCallContext(context.Background(), "c", event.Discard, nil, false),
		json.RawMessage(`{"prompt":"x","profile":"p","tools":["write_file"]}`))
	if err == nil || !strings.Contains(err.Error(), "intersection") {
		t.Fatalf("err = %v", err)
	}
}

func TestTaskResolveProfilePrecedence(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "global-m", "global-e", nil).
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
