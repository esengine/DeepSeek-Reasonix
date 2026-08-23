package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type toolResultPageHeader struct {
	ResultRef  string `json:"result_ref"`
	Offset     int    `json:"offset"`
	NextOffset int    `json:"next_offset"`
	TotalBytes int    `json:"total_bytes"`
	SHA256     string `json:"sha256"`
	Complete   bool   `json:"complete"`
}

func executeToolResultPage(t *testing.T, proxy tool.Tool, callID, ref string, offset, limit int) (toolResultPageHeader, string, error) {
	t.Helper()
	args, _ := json.Marshal(map[string]any{
		"action": "call", "capability_id": sessionToolResultCapabilityID,
		"arguments": map[string]any{"tool_call_id": callID, "result_ref": ref, "offset": offset, "limit": limit},
	})
	out, err := proxy.Execute(context.Background(), args)
	if err != nil {
		return toolResultPageHeader{}, "", err
	}
	headerText, body, ok := strings.Cut(out, "\n")
	if !ok {
		t.Fatalf("page result has no metadata header: %.200q", out)
	}
	var header toolResultPageHeader
	if err := json.Unmarshal([]byte(headerText), &header); err != nil {
		t.Fatalf("decode page header %q: %v", headerText, err)
	}
	return header, body, nil
}

func newToolResultCapabilityAgent(t *testing.T, session *Session) (*Agent, *UseCapabilityTool) {
	t.Helper()
	reg := tool.NewRegistry()
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, nil)
	reg.Add(proxy)
	a := New(nil, reg, session, Options{}, event.Discard)
	return a, proxy
}

func TestSessionToolResultPagesReconstructCompleteUTF8Output(t *testing.T) {
	full := strings.Repeat("ASCII-界-🧪\n", 6000)
	bounded, notice := truncateToolOutputFor(full, "bash", "call-1")
	if notice == "" || len(bounded) > maxToolOutputBytes {
		t.Fatalf("fixture was not bounded: bytes=%d notice=%q", len(bounded), notice)
	}
	session := &Session{Messages: []provider.Message{{
		Role: provider.RoleTool, Name: "bash", ToolCallID: "call-1", Content: bounded, RawContent: full,
	}}}
	_, proxy := newToolResultCapabilityAgent(t, session)
	ref := toolResultRef("call-1", full)
	if _, _, err := executeToolResultPage(t, proxy, "call-1", "", 0, 128); err == nil || !strings.Contains(err.Error(), "result_ref is required") {
		t.Fatalf("new truncated result accepted no result_ref: %v", err)
	}

	var rebuilt strings.Builder
	offset := 0
	for pages := 0; ; pages++ {
		if pages > 20 {
			t.Fatal("pagination did not terminate")
		}
		header, page, err := executeToolResultPage(t, proxy, "call-1", ref, offset, 0)
		if err != nil {
			t.Fatal(err)
		}
		if header.Offset != offset || header.ResultRef != ref || header.TotalBytes != len(full) || header.SHA256 == "" {
			t.Fatalf("unexpected page header: %+v", header)
		}
		if len(page) > toolResultPageDefaultBytes || len(page)+len(toolResultMustJSON(t, header))+1 > maxToolOutputBytes {
			t.Fatalf("page exceeded bounded output: page=%d", len(page))
		}
		rebuilt.WriteString(page)
		offset = header.NextOffset
		if header.Complete {
			break
		}
	}
	if got := rebuilt.String(); got != full {
		t.Fatalf("rebuilt result differs: got=%d want=%d", len(got), len(full))
	}

	header, page, err := executeToolResultPage(t, proxy, "call-1", ref, len(full), 1024)
	if err != nil || !header.Complete || page != "" || header.NextOffset != len(full) {
		t.Fatalf("terminal page = header=%+v page=%q err=%v", header, page, err)
	}
}

func TestToolResultProviderBoundaryAndRecoveryMarker(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "below", body: strings.Repeat("a", maxToolOutputBytes-1)},
		{name: "equal", body: strings.Repeat("a", maxToolOutputBytes)},
		{name: "ascii", body: strings.Repeat("a", maxToolOutputBytes+1)},
		{name: "chinese", body: strings.Repeat("界", maxToolOutputBytes)},
		{name: "emoji", body: strings.Repeat("🧪", maxToolOutputBytes)},
		{name: "error-tail", body: strings.Repeat("trace\n", 9000) + "fatal: unique error tail"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			callID := strings.Repeat("调用-🧪", 100)
			got, notice := truncateToolOutputFor(tc.body, strings.Repeat("tool-界", 100), callID)
			if len(tc.body) <= maxToolOutputBytes {
				if got != tc.body || notice != "" {
					t.Fatalf("under-bound result changed: bytes=%d notice=%q", len(got), notice)
				}
				return
			}
			if len(got) > maxToolOutputBytes || !utf8.ValidString(got) {
				t.Fatalf("bounded result invalid: bytes=%d utf8=%v", len(got), utf8.ValidString(got))
			}
			ref := toolResultRef(callID, tc.body)
			for _, want := range []string{"result_ref=" + ref, "original_bytes=", "kept_bytes=", "session:tool_result", "use_capability", "narrower arguments"} {
				if !strings.Contains(got, want) {
					t.Fatalf("recovery marker missing %q: %.1000s", want, got)
				}
			}
			if tc.name == "error-tail" && !strings.Contains(got, "fatal: unique error tail") {
				t.Fatal("error-oriented truncation lost the diagnostic tail")
			}
		})
	}
}

func toolResultMustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSessionToolResultRequiresExactRefForDuplicateCallID(t *testing.T) {
	first := strings.Repeat("first", 8000)
	second := strings.Repeat("second", 8000)
	session := &Session{Messages: []provider.Message{
		{Role: provider.RoleTool, Name: "bash", ToolCallID: "repeat", Content: "bounded-1", RawContent: first},
		{Role: provider.RoleTool, Name: "bash", ToolCallID: "repeat", Content: "bounded-2", RawContent: second},
	}}
	_, proxy := newToolResultCapabilityAgent(t, session)

	if _, _, err := executeToolResultPage(t, proxy, "repeat", "", 0, 128); err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), toolResultRef("repeat", first)) {
		t.Fatalf("missing deterministic ambiguity error: %v", err)
	}
	_, page, err := executeToolResultPage(t, proxy, "repeat", toolResultRef("repeat", first), 0, 128)
	if err != nil || page != first[:128] {
		t.Fatalf("exact ref selected wrong duplicate: page=%q err=%v", page, err)
	}
}

func TestSessionToolResultValidatesLegacyAndPagingErrors(t *testing.T) {
	fullLost := strings.Repeat("lost-new-result", 3000)
	boundedLost, notice := truncateToolOutputFor(fullLost, "read_file", "lost-new")
	if notice == "" {
		t.Fatal("new lost-result fixture was not truncated")
	}
	session := &Session{Messages: []provider.Message{
		{Role: provider.RoleTool, Name: "read_file", ToolCallID: "complete", Content: "完整结果"},
		{Role: provider.RoleTool, Name: "read_file", ToolCallID: "lost", Content: "…[truncated tool=read_file call_id=lost]…"},
		{Role: provider.RoleTool, Name: "read_file", ToolCallID: "lost-new", Content: boundedLost},
	}}
	_, proxy := newToolResultCapabilityAgent(t, session)

	_, page, err := executeToolResultPage(t, proxy, "complete", "", 0, 1024)
	if err != nil || page != "完整结果" {
		t.Fatalf("legacy complete result = %q err=%v", page, err)
	}
	if _, _, err := executeToolResultPage(t, proxy, "lost", "", 0, 1024); err == nil || !strings.Contains(err.Error(), "full result is unavailable") {
		t.Fatalf("legacy truncated result error = %v", err)
	}
	fullLostRef := toolResultRef("lost-new", fullLost)
	if _, _, err := executeToolResultPage(t, proxy, "lost-new", "", 0, 1024); err == nil || !strings.Contains(err.Error(), fullLostRef) {
		t.Fatalf("new truncated result did not request its marker ref: %v", err)
	}
	if _, _, err := executeToolResultPage(t, proxy, "lost-new", fullLostRef, 0, 1024); err == nil || !strings.Contains(err.Error(), "full result is unavailable") {
		t.Fatalf("new truncated result with missing RawContent error = %v", err)
	}
	if _, _, err := executeToolResultPage(t, proxy, "complete", "", 1, 1024); err == nil || !strings.Contains(err.Error(), "UTF-8 character boundary") {
		t.Fatalf("invalid UTF-8 offset error = %v", err)
	}
	if _, _, err := executeToolResultPage(t, proxy, "complete", "", 0, toolResultPageMaxBytes+1); err == nil || !strings.Contains(err.Error(), "limit must be") {
		t.Fatalf("invalid limit error = %v", err)
	}
	if _, _, err := executeToolResultPage(t, proxy, "missing", "", 0, 10); err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("missing result error = %v", err)
	}
}

func TestSessionToolResultBindingTracksSetSessionAndCloneIsIsolated(t *testing.T) {
	parent := &Session{Messages: []provider.Message{{Role: provider.RoleTool, ToolCallID: "call", Name: "bash", Content: "parent"}}}
	a, proxy := newToolResultCapabilityAgent(t, parent)
	clone := proxy.CloneForAgent(nil, nil)
	if clone.currentToolResultTarget() != nil {
		t.Fatal("CloneForAgent inherited the parent session reader")
	}
	childRegistry := tool.NewRegistry()
	childRegistry.Add(clone)
	_ = New(nil, childRegistry, &Session{Messages: []provider.Message{{Role: provider.RoleTool, ToolCallID: "call", Name: "bash", Content: "child"}}}, Options{}, event.Discard)

	_, page, err := executeToolResultPage(t, proxy, "call", "", 0, 32)
	if err != nil || page != "parent" {
		t.Fatalf("parent page=%q err=%v", page, err)
	}
	a.SetSession(&Session{Messages: []provider.Message{{Role: provider.RoleTool, ToolCallID: "call", Name: "bash", Content: "replacement"}}})
	_, page, err = executeToolResultPage(t, proxy, "call", "", 0, 32)
	if err != nil || page != "replacement" {
		t.Fatalf("replacement page=%q err=%v", page, err)
	}
	_, page, err = executeToolResultPage(t, clone, "call", "", 0, 32)
	if err != nil || page != "child" {
		t.Fatalf("child page=%q err=%v", page, err)
	}
}

func TestSessionToolResultBindingDoesNotChangeUseCapabilitySchema(t *testing.T) {
	reg := tool.NewRegistry()
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, nil)
	reg.Add(proxy)
	beforeName, beforeDescription := proxy.Name(), proxy.Description()
	beforeSchema := append([]byte(nil), proxy.Schema()...)
	beforeProviderSchemas := reg.Schemas()

	_ = New(nil, reg, NewSession("system"), Options{}, event.Discard)
	if proxy.Name() != beforeName || proxy.Description() != beforeDescription || !reflect.DeepEqual(proxy.Schema(), json.RawMessage(beforeSchema)) {
		t.Fatal("session reader binding changed the stable use_capability contract")
	}
	if !reflect.DeepEqual(reg.Schemas(), beforeProviderSchemas) {
		t.Fatal("session reader binding changed provider tool schemas")
	}
	list, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil || !strings.Contains(list, sessionToolResultCapabilityID) {
		t.Fatalf("bound capability missing from list: err=%v list=%s", err, list)
	}
	inspectArgs := json.RawMessage(`{"action":"inspect","capability_id":"session:tool_result"}`)
	inspect, err := proxy.Execute(context.Background(), inspectArgs)
	if err != nil || !strings.Contains(inspect, `"limit_max": 24576`) {
		t.Fatalf("inspect result: err=%v out=%s", err, inspect)
	}
}

func TestRestrictedCapabilityProxyListsAndReadsOnlyOwnToolResults(t *testing.T) {
	inner := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), nil, nil, nil)
	resolver := tool.CallResolver(inner)
	proxy := &restrictedCapabilityProxy{
		Tool: inner, resolver: resolver,
		allowed: map[string]bool{"mcp-server:allowed": true}, servers: map[string]bool{"allowed": true},
	}
	reg := tool.NewRegistry()
	reg.Add(proxy)
	_ = New(nil, reg, &Session{Messages: []provider.Message{{Role: provider.RoleTool, Name: "read", ToolCallID: "own", Content: "own result"}}}, Options{}, event.Discard)

	list, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil || !strings.Contains(list, sessionToolResultCapabilityID) {
		t.Fatalf("restricted list omitted session capability: err=%v list=%s", err, list)
	}
	args := json.RawMessage(`{"action":"call","capability_id":"session:tool_result","arguments":{"tool_call_id":"own"}}`)
	out, err := proxy.Execute(context.Background(), args)
	if err != nil || !strings.HasSuffix(out, "\nown result") {
		t.Fatalf("restricted self read: err=%v out=%q", err, out)
	}
}

func TestRestrictedCapabilityFrontendCloneKeepsAgentSessionsIsolated(t *testing.T) {
	parentInner := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), nil, nil, nil)
	parentProxy := &restrictedCapabilityProxy{
		Tool: parentInner, resolver: parentInner,
		allowed: map[string]bool{"mcp-server:allowed": true}, servers: map[string]bool{"allowed": true},
	}
	parentRegistry := tool.NewRegistry()
	parentRegistry.Add(parentProxy)
	_ = New(nil, parentRegistry, &Session{Messages: []provider.Message{{
		Role: provider.RoleTool, ToolCallID: "call", Name: "read", Content: "parent",
	}}}, Options{}, event.Discard)

	childTool := newSubagentCapabilityFrontend(parentRegistry, nil)
	childProxy, ok := childTool.(*restrictedCapabilityProxy)
	if !ok {
		t.Fatalf("child frontend type = %T, want *restrictedCapabilityProxy", childTool)
	}
	if childProxy == parentProxy || childProxy.Tool == parentProxy.Tool {
		t.Fatal("child restricted frontend shares the parent's session-bindable proxy")
	}
	childRegistry := tool.NewRegistry()
	childRegistry.Add(childProxy)
	_ = New(nil, childRegistry, &Session{Messages: []provider.Message{{
		Role: provider.RoleTool, ToolCallID: "call", Name: "read", Content: "child",
	}}}, Options{}, event.Discard)

	_, parentPage, parentErr := executeToolResultPage(t, parentProxy, "call", "", 0, 32)
	_, childPage, childErr := executeToolResultPage(t, childProxy, "call", "", 0, 32)
	if parentErr != nil || parentPage != "parent" {
		t.Fatalf("parent page=%q err=%v", parentPage, parentErr)
	}
	if childErr != nil || childPage != "child" {
		t.Fatalf("child page=%q err=%v", childPage, childErr)
	}
	childProxy.allowed["mcp-server:child-only"] = true
	if parentProxy.allowed["mcp-server:child-only"] {
		t.Fatal("child clone shares the parent's mutable capability allowlist")
	}
	beforeRegistry := tool.NewRegistry()
	beforeRegistry.Add(parentProxy)
	afterRegistry := tool.NewRegistry()
	afterRegistry.Add(childProxy)
	if parentProxy.Name() != childProxy.Name() || parentProxy.Description() != childProxy.Description() ||
		!reflect.DeepEqual(parentProxy.Schema(), childProxy.Schema()) ||
		!reflect.DeepEqual(beforeRegistry.Schemas(), afterRegistry.Schemas()) {
		t.Fatal("cloning or binding changed provider-visible use_capability bytes")
	}
}

func TestPathBoundCapabilityFrontendBindsAndClonesAgentSessions(t *testing.T) {
	for _, restricted := range []bool{false, true} {
		name := "unrestricted"
		if restricted {
			name = "restricted"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			claim, err := NormalizeWritePaths(root, []string{"frontend"})
			if err != nil {
				t.Fatal(err)
			}
			baseRegistry := tool.NewRegistry()
			inner := NewUseCapabilityTool(context.Background(), nil, nil, baseRegistry, nil, nil, nil)
			var frontend tool.Tool = inner
			if restricted {
				frontend = &restrictedCapabilityProxy{
					Tool: inner, resolver: inner,
					allowed: map[string]bool{"mcp-server:allowed": true}, servers: map[string]bool{"allowed": true},
				}
			}
			baseRegistry.Add(frontend)
			parentRegistry, removed := BindWritePaths(baseRegistry, claim, root, false)
			if len(removed) != 0 {
				t.Fatalf("path-bound registry removed tools: %v", removed)
			}
			parentTool, ok := parentRegistry.Get("use_capability")
			if !ok {
				t.Fatal("path-bound registry missing use_capability")
			}
			_ = New(nil, parentRegistry, &Session{Messages: []provider.Message{{
				Role: provider.RoleTool, ToolCallID: "call", Name: "read", Content: "parent",
			}}}, Options{}, event.Discard)

			childTool := newSubagentCapabilityFrontend(parentRegistry, nil)
			if childTool == nil {
				t.Fatal("path-bound child frontend was dropped during clone")
			}
			childRegistry := tool.NewRegistry()
			childRegistry.Add(childTool)
			_ = New(nil, childRegistry, &Session{Messages: []provider.Message{{
				Role: provider.RoleTool, ToolCallID: "call", Name: "read", Content: "child",
			}}}, Options{}, event.Discard)

			_, parentPage, parentErr := executeToolResultPage(t, parentTool, "call", "", 0, 32)
			_, childPage, childErr := executeToolResultPage(t, childTool, "call", "", 0, 32)
			if parentErr != nil || parentPage != "parent" {
				t.Fatalf("path-bound parent page=%q err=%v", parentPage, parentErr)
			}
			if childErr != nil || childPage != "child" {
				t.Fatalf("path-bound child page=%q err=%v", childPage, childErr)
			}
			if parentTool.Name() != childTool.Name() || parentTool.Description() != childTool.Description() ||
				!reflect.DeepEqual(parentTool.Schema(), childTool.Schema()) ||
				!reflect.DeepEqual(parentRegistry.Schemas(), childRegistry.Schemas()) {
				t.Fatal("path-bound cloning or binding changed provider-visible use_capability bytes")
			}
		})
	}
}

func TestPlannerRestrictedCapabilityFrontendCloneKeepsExecutorSession(t *testing.T) {
	inner := NewUseCapabilityTool(context.Background(), nil, nil, tool.NewRegistry(), nil, nil, nil)
	executorProxy := &restrictedCapabilityProxy{
		Tool: inner, resolver: inner,
		allowed: map[string]bool{"mcp-server:allowed": true}, servers: map[string]bool{"allowed": true},
	}
	parent := tool.NewRegistry()
	parent.Add(executorProxy)
	_ = New(nil, parent, &Session{Messages: []provider.Message{{
		Role: provider.RoleTool, ToolCallID: "call", Name: "read", Content: "executor",
	}}}, Options{}, event.Discard)

	plannerRegistry := PlannerToolRegistry(parent)
	plannerTool, ok := plannerRegistry.Get("use_capability")
	if !ok {
		t.Fatal("planner missing use_capability")
	}
	plannerProxy, ok := plannerTool.(*restrictedCapabilityProxy)
	if !ok || plannerProxy == executorProxy || plannerProxy.Tool == executorProxy.Tool {
		t.Fatalf("planner frontend was not deeply cloned: %T", plannerTool)
	}
	_ = New(nil, plannerRegistry, &Session{Messages: []provider.Message{{
		Role: provider.RoleTool, ToolCallID: "call", Name: "read", Content: "planner",
	}}}, Options{}, event.Discard)

	_, executorPage, executorErr := executeToolResultPage(t, executorProxy, "call", "", 0, 32)
	_, plannerPage, plannerErr := executeToolResultPage(t, plannerProxy, "call", "", 0, 32)
	if executorErr != nil || executorPage != "executor" {
		t.Fatalf("executor page=%q err=%v", executorPage, executorErr)
	}
	if plannerErr != nil || plannerPage != "planner" {
		t.Fatalf("planner page=%q err=%v", plannerPage, plannerErr)
	}
}

func TestProviderRequestsNeverUploadToolRawContent(t *testing.T) {
	const rawSentinel = "RAW-SENTINEL-MUST-STAY-LOCAL"
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "read", Content: "bounded", RawContent: rawSentinel},
	}
	a := New(nil, tool.NewRegistry(), &Session{Messages: msgs}, Options{}, event.Discard)
	req := a.summaryRequest(msgs, "")
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), rawSentinel) || !strings.Contains(string(b), "bounded") {
		t.Fatalf("summary request leaked RawContent: %s", b)
	}
	projected := buildVisibleCompressionProjection(msgs, visibleCompressionPlan{foldMask: make([]bool, len(msgs)), firstFold: -1}, "")
	b, _ = json.Marshal(projected)
	if strings.Contains(string(b), rawSentinel) || !strings.Contains(string(b), "bounded") {
		t.Fatalf("projection leaked RawContent: %s", b)
	}
}

func TestSessionToolResultRejectsPageThatSplitsUTF8Rune(t *testing.T) {
	session := &Session{Messages: []provider.Message{{Role: provider.RoleTool, Name: "read", ToolCallID: "utf8", Content: "界"}}}
	_, proxy := newToolResultCapabilityAgent(t, session)
	if _, _, err := executeToolResultPage(t, proxy, "utf8", "", 0, 1); err == nil || !strings.Contains(err.Error(), "increase limit") {
		t.Fatalf("split-rune limit error = %v", err)
	}
}

func TestSessionToolResultSHA256MatchesCompleteBody(t *testing.T) {
	body := strings.Repeat("hash-界", 100)
	session := &Session{Messages: []provider.Message{{Role: provider.RoleTool, Name: "read", ToolCallID: "hash", Content: body}}}
	_, proxy := newToolResultCapabilityAgent(t, session)
	header, page, err := executeToolResultPage(t, proxy, "hash", "", 0, toolResultPageMaxBytes)
	if err != nil || page != body {
		t.Fatalf("page=%q err=%v", page, err)
	}
	digest := sha256.Sum256([]byte(body))
	if !bytes.Equal(mustDecodeHex(t, header.SHA256), digest[:]) {
		t.Fatalf("sha256=%q does not match complete body", header.SHA256)
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	b, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
