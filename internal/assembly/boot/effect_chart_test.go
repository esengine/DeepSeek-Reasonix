package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/chartspec"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

const chartSpecJSON = `{"spec_version":1,"title":"Sales","data":{"columns":[{"name":"m","type":"string"},{"name":"v","type":"number"}],"rows":[["jan",1],["feb",3]]},"marks":[{"type":"bar","x":"m","y":["v"]}]}`

func chartCall(t *testing.T, id string, spec string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"action": "call", "capability_id": id, "arguments": json.RawMessage(spec)})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func buildChartAssembly(t *testing.T, kind string, probe *capabilityProbeProvider, dir string) *control.Controller {
	t.Helper()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return probe, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, WorkspaceRoot: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ctrl
}

// The chart tool is a registered built-in that the provider surface leaves out:
// the first request's tools and system prompt carry nothing of it, it is found
// and called through use_capability, a hostile spec is refused by code, and the
// session that ran it resumes with the spec in the call that carried it.
func TestEffectRenderChartHiddenDiscoverableAndPersisted(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	hostile := strings.Replace(chartSpecJSON, `"title"`, `"url":"http://x.test","title"`, 1)
	probe := &capabilityProbeProvider{calls: []string{
		`{"action":"search","query":"chart"}`,
		`{"action":"inspect","capability_id":"tool:render_chart"}`,
		chartCall(t, "tool:render_chart", chartSpecJSON),
		chartCall(t, "tool:render_chart", hostile),
	}}
	ctrl := buildChartAssembly(t, "boot-chart-effect", probe, dir)
	path := filepath.Join(dir, ".reasonix", "sessions", "chart.jsonl")
	ctrl.SetSessionPath(path)
	if err := ctrl.Run(context.Background(), "chart the sales"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()

	probe.mu.Lock()
	first := probe.reqs[0]
	probe.mu.Unlock()
	var names []string
	for _, s := range first.Tools {
		names = append(names, s.Name)
		if s.Name == "render_chart" || strings.Contains(string(s.Parameters), "spec_version") {
			t.Fatalf("the provider schema carries the chart tool: %s", s.Name)
		}
	}
	if !slices.Contains(names, "use_capability") {
		t.Fatalf("use_capability missing from %v", names)
	}
	for _, m := range first.Messages {
		if strings.Contains(m.Content, "render_chart") || strings.Contains(m.Content, "chartspec") {
			t.Fatalf("the prefix mentions the chart tool: %q", m.Content)
		}
	}

	results := probe.toolResults()
	if len(results) != 4 {
		t.Fatalf("want 4 tool results, got %d: %v", len(results), results)
	}
	if !strings.Contains(results[0], "tool:render_chart") {
		t.Errorf("search for chart does not surface the tool:\n%s", results[0])
	}
	if !strings.Contains(results[1], "spec_version") {
		t.Errorf("inspect does not hand back the schema:\n%s", results[1])
	}
	if !strings.Contains(results[2], "chart_id: chart-") || strings.Contains(results[2], "jan") {
		t.Errorf("a valid call must return an id and a summary without rows:\n%s", results[2])
	}
	if !strings.Contains(results[3], string(chartspec.CodeSchemaInvalid)) || strings.Contains(results[3], "chart_id") {
		t.Errorf("a hostile spec must be refused by code:\n%s", results[3])
	}

	loaded, err := sessionstore.LoadSession(path)
	if err != nil || loaded == nil {
		t.Fatalf("load: %v", err)
	}
	if got := persistedCharts(t, loaded.Messages); len(got) != 1 {
		t.Fatalf("exactly the one valid spec must replay as a chart, got %d", len(got))
	}

	resume := &capabilityProbeProvider{}
	ctrl = buildChartAssembly(t, "boot-chart-effect-resume", resume, dir)
	defer ctrl.Close()
	if err := ctrl.Resume(loaded, path); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := ctrl.Run(context.Background(), "and now?"); err != nil {
		t.Fatalf("Run after resume: %v", err)
	}
	resume.mu.Lock()
	defer resume.mu.Unlock()
	if got := persistedCharts(t, resume.reqs[0].Messages); len(got) != 1 || got[0].Title != "Sales" {
		t.Fatalf("the resumed request does not restore the spec: %v", got)
	}
}

// persistedCharts rebuilds charts the way a frontend does: from the arguments
// of use_capability calls that resolved to render_chart, accepting only specs
// that still validate.
func persistedCharts(t *testing.T, msgs []provider.Message) []*chartspec.Spec {
	t.Helper()
	var out []*chartspec.Spec
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			var a struct {
				Action string          `json:"action"`
				ID     string          `json:"capability_id"`
				Args   json.RawMessage `json:"arguments"`
			}
			if tc.Name != "use_capability" || json.Unmarshal([]byte(tc.Arguments), &a) != nil || a.Action != "call" || a.ID != "tool:render_chart" {
				continue
			}
			if spec, err := chartspec.Parse(a.Args); err == nil {
				out = append(out, spec)
			}
		}
	}
	return out
}
