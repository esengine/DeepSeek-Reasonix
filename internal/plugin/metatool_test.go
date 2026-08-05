package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

// metatool_test.go validates the PRODUCTION MetaTool (not the stub in
// meta_capacity_test.go) against the same description contract, plus Execute's
// argument validation and error shape. The Description() path is exercised via
// the cached-schema fallback (no live subprocess): a fresh Host has no
// connected clients, so snapshotServerTools falls through to LoadCachedSchema —
// the same path boot takes on the first turn before spawns complete.

// TestMetaToolProductionDescriptionContract runs the production MetaTool through
// the 8 contract points locked by TestMetaToolDescriptionListsServerToolMapping
// for the stub. If production drifts from the stub format, the model-facing
// anti-confusion guarantees (quoted server_name, non-swappable args) break, so
// this test fails fast.
func TestMetaToolProductionDescriptionContract(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	writeMockCache(t, spec) // echo + zed under server "mock"

	mt := NewMetaTool(NewHost(), []Spec{spec})
	desc := mt.Description()

	// 1-2. Every server_name and tool_name appears.
	if !strings.Contains(desc, "mock") {
		t.Errorf("描述缺少 server_name %q\n描述:\n%s", "mock", desc)
	}
	for _, tl := range []string{"echo", "zed"} {
		if !strings.Contains(desc, tl) {
			t.Errorf("描述缺少 tool_name %q\n描述:\n%s", tl, desc)
		}
	}
	// 3. Field semantics labeled.
	if !strings.Contains(desc, "server_name") || !strings.Contains(desc, "tool_name") {
		t.Errorf("描述未标注 server_name/tool_name 字段\n描述:\n%s", desc)
	}
	// 4. server_name quoted (%q), visually distinct from bare tool_names.
	if !strings.Contains(desc, `"mock"`) {
		t.Errorf("描述中 server_name 未带引号标注\n描述:\n%s", desc)
	}
	// 5. Non-swappable args hint present.
	if !strings.Contains(desc, "Do not swap") {
		t.Errorf("描述缺少「参数不可互换」提示\n描述:\n%s", desc)
	}
	// 6. Empty surface (no specs, no clients) is graceful, not a panic.
	empty := NewMetaTool(NewHost(), nil).Description()
	if !strings.Contains(empty, "No MCP servers") {
		t.Errorf("空映射描述应提示无服务器, got:\n%s", empty)
	}
	// 7. Dynamic: different specs produce different descriptions.
	other := Spec{Name: "other-srv"}
	t.Run("dynamic", func(t *testing.T) {
		// other-srv has no cache, so its description is the empty-surface one —
		// distinct from the mock-srv description. This proves non-hardcoding.
		a := NewMetaTool(NewHost(), []Spec{spec}).Description()
		b := NewMetaTool(NewHost(), []Spec{other}).Description()
		if a == b {
			t.Error("描述非动态: 不同 specs 产出相同描述")
		}
	})
	// 8. Stable: same input → same bytes (sorted, no map-order leakage).
	a := NewMetaTool(NewHost(), []Spec{spec}).Description()
	b := NewMetaTool(NewHost(), []Spec{spec}).Description()
	if a != b {
		t.Error("描述不稳定: 同一映射两次调用产出不同结果")
	}
	t.Logf("生产 run_mcp 描述示例:\n%s", desc)
}

// TestMetaToolSchemaIsValid verifies the fixed schema parses and exposes the
// three contract fields. Execute reads server_name/tool_name/args by these
// exact JSON keys, so a schema drift would silently break dispatch.
func TestMetaToolSchemaIsValid(t *testing.T) {
	var s struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(NewMetaTool(nil, nil).Schema(), &s); err != nil {
		t.Fatalf("Schema 不是合法 JSON: %v", err)
	}
	if s.Type != "object" {
		t.Errorf("Schema type=%q, want object", s.Type)
	}
	for _, k := range []string{"server_name", "tool_name", "args"} {
		if _, ok := s.Properties[k]; !ok {
			t.Errorf("Schema 缺少 property %q", k)
		}
	}
	for _, r := range []string{"server_name", "tool_name"} {
		found := false
		for _, got := range s.Required {
			if got == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Schema required 缺少 %q", r)
		}
	}
}

// TestMetaToolExecuteArgValidation locks the Execute error shape so a malformed
// model call produces an actionable message naming the missing field, not a
// opaque dispatch failure. No subprocess is started — validation runs before
// the Host lookup.
func TestMetaToolExecuteArgValidation(t *testing.T) {
	mt := NewMetaTool(NewHost(), nil)

	// Missing both server_name and tool_name.
	if _, err := mt.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Error("空 args 应返回错误")
	} else if !strings.Contains(err.Error(), "server_name") || !strings.Contains(err.Error(), "tool_name") {
		t.Errorf("错误未点名缺失字段, got: %v", err)
	}

	// Unknown server: error must name the server and hint at availability so the
	// model can self-correct a typo vs. retry a not-yet-spawned server.
	if _, err := mt.Execute(context.Background(), json.RawMessage(`{"server_name":"nope","tool_name":"echo"}`)); err == nil {
		t.Error("未知 server 应返回错误")
	} else if !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "available") {
		t.Errorf("错误应含服务器名与 available 提示, got: %v", err)
	}

	// Malformed JSON.
	if _, err := mt.Execute(context.Background(), json.RawMessage(`{not json`)); err == nil {
		t.Error("非法 JSON 应返回错误")
	}
}

// TestMetaToolReadOnlyFalse confirms run_mcp is treated as a writer (it can
// dispatch to side-effecting MCP tools), so it never joins a read-only parallel
// batch and plan-mode fail-closes on it.
func TestMetaToolReadOnlyFalse(t *testing.T) {
	if NewMetaTool(nil, nil).ReadOnly() {
		t.Error("run_mcp ReadOnly()=true, want false (dispatches to writer tools)")
	}
}

// TestMetaToolImplementsImageTool ensures MCP image content reaches vision
// models. The agent's executor type-asserts to tool.ImageTool; dropping the
// interface would silently degrade images to text placeholders.
func TestMetaToolImplementsImageTool(t *testing.T) {
	var _ tool.ImageTool = (*MetaTool)(nil)
}

