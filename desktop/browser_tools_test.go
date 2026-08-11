package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/tool"

	"reasonix/desktop/internal/browseripc"
)

// TestBrowserToolSchemasGolden pins the two provider-visible tool schemas:
// any change to browser_read/browser_act must be deliberate (cache-prefix
// impact on every desktop session).
func TestBrowserToolSchemasGolden(t *testing.T) {
	var read, act map[string]any
	if err := json.Unmarshal([]byte(browserReadSchema), &read); err != nil {
		t.Fatalf("browser_read schema does not parse: %v", err)
	}
	if err := json.Unmarshal([]byte(browserActSchema), &act); err != nil {
		t.Fatalf("browser_act schema does not parse: %v", err)
	}
	props, ok := read["properties"].(map[string]any)
	if !ok {
		t.Fatal("browser_read properties missing")
	}
	action, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatal("browser_read action missing")
	}
	enum, _ := action["enum"].([]any)
	got := make([]string, len(enum))
	for i, v := range enum {
		got[i], _ = v.(string)
	}
	if strings.Join(got, ",") != "list_tabs,open,navigate,snapshot,screenshot,wait" {
		t.Fatalf("browser_read actions = %v", got)
	}
	if _, ok := read["required"]; !ok {
		t.Fatal("browser_read must require action")
	}

	propsAct, _ := act["properties"].(map[string]any)
	actAction, _ := propsAct["action"].(map[string]any)
	actEnum, _ := actAction["enum"].([]any)
	actGot := make([]string, len(actEnum))
	for i, v := range actEnum {
		actGot[i], _ = v.(string)
	}
	if strings.Join(actGot, ",") != "click,hover,scroll,type,press,select" {
		t.Fatalf("browser_act actions = %v", actGot)
	}
}

// TestBrowserHostToolsForTab: the pair is fixed, ordered, scoped to the tab,
// and classified correctly for plan mode and host mutation.
func TestBrowserHostToolsForTab(t *testing.T) {
	a := testBrowserApp()
	tools := a.browserHostToolsForTab("chat-1")
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
	if tools[0].Name != "browser_read" || tools[1].Name != "browser_act" {
		t.Fatalf("tool order: %s, %s", tools[0].Name, tools[1].Name)
	}
	for _, ht := range tools {
		if ht.Source != "browser" {
			t.Fatalf("%s source = %q, want browser", ht.Name, ht.Source)
		}
		if len(ht.Schema) == 0 {
			t.Fatalf("%s has no schema", ht.Name)
		}
	}
	if !tools[0].ReadOnly || !tools[0].PlanModeSafe || !tools[0].HostMutation {
		t.Fatalf("browser_read classifiers: readOnly=%v planSafe=%v hostMutation=%v", tools[0].ReadOnly, tools[0].PlanModeSafe, tools[0].HostMutation)
	}
	if tools[1].ReadOnly || tools[1].PlanModeSafe {
		t.Fatalf("browser_act must be a non-plan writer: %+v", tools[1])
	}
	if !a.browserToolsEnabled.Load() {
		t.Fatal("browserToolsEnabled not set")
	}
	// Unknown/empty tab: no tools, no flag.
	a.browserToolsEnabled.Store(false)
	if got := a.browserHostToolsForTab(""); got != nil {
		t.Fatalf("empty tab must not get tools: %v", got)
	}
}

// TestBrowserReadToolSurface: the tool adapter round-trips schema, read-only
// flags, and plan-mode classifiers through tool.NewHostTool.
func TestBrowserReadToolSurface(t *testing.T) {
	a := testBrowserApp()
	ht := a.browserHostToolsForTab("chat-1")[0]
	adapter := tool.NewHostTool(ht)
	if !adapter.ReadOnly() {
		t.Fatal("browser_read must be read-only")
	}
	if c, ok := adapter.(tool.PlanModeClassifier); !ok || !c.PlanModeSafe() {
		t.Fatal("browser_read must be plan-mode safe")
	}
	if m, ok := adapter.(tool.ReadOnlyExecutionHostMutation); !ok || !m.ReadOnlyExecutionHostMutation() {
		t.Fatal("browser_read must declare host mutation")
	}
	if _, ok := adapter.(tool.ImageTool); !ok {
		t.Fatal("browser_read must expose the image channel for screenshots")
	}
}

// TestBrowserReadActionsRouteThroughCoordinator: with a missing companion
// every action surfaces the actionable component-missing message, never a raw
// internal error.
func TestBrowserReadActionsRouteThroughCoordinator(t *testing.T) {
	a := testBrowserApp()
	read := &browserReadTool{ownerID: func() string { return "chat-1" }, app: a}
	for _, args := range []string{
		`{"action":"list_tabs"}`,
		`{"action":"open","url":"https://example.com"}`,
		`{"action":"navigate","tab_id":"t1","url":"https://example.com"}`,
		`{"action":"snapshot","tab_id":"t1"}`,
		`{"action":"wait","tab_id":"t1","wait_until":"load"}`,
		`{"action":"screenshot","tab_id":"t1"}`,
	} {
		text, images, err := read.ExecuteWithImages(context.Background(), json.RawMessage(args))
		if err == nil || !strings.Contains(err.Error(), "not installed") {
			t.Errorf("args %s: err = %v, want component-missing guidance", args, err)
		}
		if images != nil {
			t.Errorf("args %s: unexpected images %v", args, images)
		}
		_ = text
	}
}

// TestBrowserActRequiresTabAndAction: invalid invocations fail closed before
// any coordinator contact.
func TestBrowserActRequiresTabAndAction(t *testing.T) {
	a := testBrowserApp()
	act := &browserActTool{ownerID: func() string { return "chat-1" }, app: a}
	if _, err := act.Execute(context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "tab_id") {
		t.Fatalf("empty args: %v", err)
	}
	if _, err := act.Execute(context.Background(), json.RawMessage(`{"tab_id":"t1","action":"eval"}`)); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown action: %v", err)
	}
	if _, err := act.Execute(context.Background(), json.RawMessage(`{"tab_id":"t1","action":"click"}`)); err == nil {
		t.Fatal("click without companion should fail")
	}
}

// TestBrowserToolErrorMapping: wire error codes translate to actionable,
// page-content-free messages.
func TestBrowserToolErrorMapping(t *testing.T) {
	cases := []struct {
		code browseripc.ErrorCode
		want string
	}{
		{browseripc.CodeComponentMissing, "not installed"},
		{browseripc.CodeNotReady, "starting"},
		{browseripc.CodeCrashed, "crashed"},
		{browseripc.CodeTabBusy, "busy"},
		{browseripc.CodeStaleRef, "fresh snapshot"},
		{browseripc.CodeUserTakeoverReq, "user"},
		{browseripc.CodeUserTookControl, "took control"},
		{browseripc.CodeTabNotFound, "no longer open"},
		{browseripc.CodeTimeout, "retry"},
		{browseripc.CodeUnsupported, "not implemented"},
	}
	for _, tc := range cases {
		err := browserToolError(browseripcCodeError(tc.code, "detail"))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("code %s: err = %v, want substring %q", tc.code, err, tc.want)
		}
	}
	if err := browserToolError(ErrBrowserDisabled); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled: %v", err)
	}
	if err := browserToolError(errors.New("plain")); err == nil || err.Error() != "plain" {
		t.Errorf("unknown error must pass through: %v", err)
	}
}

// TestBrowserSnapshotTextFormat: snapshot results format title/url/generation
// and the compact tree/text without page content leaking into telemetry.
func TestBrowserSnapshotTextFormat(t *testing.T) {
	res := &browseripc.TabSnapshotResult{
		TabID: "t1", URL: "https://example.com", Title: "Example", Generation: 3,
		Snapshot: browseripc.SnapshotData{Tree: "root\n  button [r1]", Text: "hello", Truncated: true},
	}
	out := browserSnapshotText(res)
	for _, want := range []string{"t1", "https://example.com", "Example", "generation 3", "[r1]", "truncated"} {
		if !strings.Contains(out, want) {
			t.Errorf("snapshot text missing %q: %s", want, out)
		}
	}
}
