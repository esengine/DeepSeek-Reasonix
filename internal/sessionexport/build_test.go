package sessionexport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/attachment"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/sessioncontent"
	"strings"
	"testing"
)

const exportAttachmentPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestBuildStagesAndRendersAuthorizedImageInputs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	service, err := session.NewService("local", session.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	defer service.CloseAll(context.Background())
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "images"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(exportAttachmentPNG)
	if err != nil {
		t.Fatal(err)
	}
	contentRef, err := runtime.Session().ContentStore().Put(t.Context(), bytes.NewReader(raw), sessioncontent.Metadata{MediaType: "image/png", Name: "shot.png"})
	if err != nil {
		t.Fatal(err)
	}
	message := provider.Message{ID: "user-image", Role: provider.RoleUser, Content: "inspect", Images: []string{"https://example.test/legacy.png"}, ImageInputs: []attachment.ImageInput{{
		Kind:       attachment.KindAttachment,
		Attachment: &attachment.AttachmentRef{Version: attachment.RefVersion, Content: contentRef, Width: 1, Height: 1, DisplayName: "shot.png"},
	}}}
	payload, _ := json.Marshal(map[string]any{"message": message})
	if _, err = runtime.Session().AppendBatch(t.Context(), "user-image", []session.Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Query().CaptureExportSnapshot(t.Context(), runtime.Ref())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err = Build(t.Context(), service.Query(), snapshot, dir, nil); err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(filepath.Join(dir, "attachments", contentRef.Digest))
	if err != nil || !bytes.Equal(staged, raw) {
		t.Fatalf("staged attachment mismatch: %v", err)
	}
	markdown, err := os.ReadFile(filepath.Join(dir, "markdown"))
	if err != nil {
		t.Fatal(err)
	}
	wantDataURL := attachment.DataURL("image/png", raw)
	if !strings.Contains(string(markdown), "![attachment](https://example.test/legacy.png)") || !strings.Contains(string(markdown), "![attachment]("+wantDataURL+")") {
		t.Fatalf("markdown did not preserve legacy and durable images: %s", markdown)
	}
	var document struct {
		Items []Item `json:"items"`
	}
	jsonBody, err := os.ReadFile(filepath.Join(dir, "json"))
	if err != nil || json.Unmarshal(jsonBody, &document) != nil {
		t.Fatalf("read json export: %v", err)
	}
	if len(document.Items) != 1 {
		t.Fatalf("items = %d", len(document.Items))
	}
	images, ok := document.Items[0]["images"].([]any)
	if !ok || len(images) != 2 || images[0] != "https://example.test/legacy.png" || images[1] != wantDataURL {
		t.Fatalf("export images = %#v", document.Items[0]["images"])
	}
}

func TestBuildFullSnapshotAcrossPagesAndLargeTools(t *testing.T) {
	service, err := session.NewService("local", session.NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	defer service.CloseAll(context.Background())
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "complete"})
	if err != nil {
		t.Fatal(err)
	}
	turnID := ""
	appendMessage := func(m provider.Message) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"message": m})
		if _, err := runtime.Session().Append(t.Context(), session.Batch{OperationID: m.ID, TurnID: turnID, Events: []session.Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 110 {
		appendMessage(provider.Message{ID: fmt.Sprintf("user-%d", i), Role: provider.RoleUser, Content: fmt.Sprintf("QUESTION-%03d", i), Origin: provider.MessageOrigin("user")})
	}
	output := "  prefix\n\n\n" + strings.Repeat("中文✓ ", 200000) + "\n``````\n suffix  \n"

	turnID = "incident"
	appendEvents := func(id string, events ...session.Event) {
		t.Helper()
		if _, err := runtime.Session().Append(t.Context(), session.Batch{OperationID: id, TurnID: turnID, Events: events}); err != nil {
			t.Fatal(err)
		}
	}
	appendEvents("turn-start", session.Event{Kind: "turn/start", Payload: json.RawMessage(`{"status":"in_progress"}`)})
	appendEvents("mcp-notice", session.Event{Kind: "diagnostic", Optional: true, Payload: json.RawMessage(`{"type":"display-notice-v1","displayRecord":{"id":"mcp-notice","role":"notice","content":"MCP tools/list","code":"mcp_tools_list","detail":"{\"source\":\"shared_host\",\"network_call\":true}"}}`)})
	callIndex := 0
	for sample := range 15 {
		attempt, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("attempt-%d", sample), "action": "begin"})
		appendEvents(fmt.Sprintf("sampling-%d", sample), session.Event{Kind: "assistant/attempt", Payload: attempt})
		count := 1
		if sample < 8 {
			count = 2
		}
		calls := make([]provider.ToolCall, 0, count)
		for range count {
			id := fmt.Sprintf("call-%d", callIndex)
			callIndex++
			calls = append(calls, provider.ToolCall{ID: id, Name: "bash", Arguments: `{"command":"echo test"}`})
		}
		appendMessage(provider.Message{ID: fmt.Sprintf("assistant-%d", sample), Role: provider.RoleAssistant, ReasoningContent: fmt.Sprintf("reasoning-%d", sample), ToolCalls: calls})
		for _, call := range calls {
			payload, _ := json.Marshal(map[string]string{"id": call.ID, "name": "bash"})
			appendEvents("dispatch-"+call.ID, session.Event{Kind: "tool/call", Payload: payload})
			appendMessage(provider.Message{ID: "result-" + call.ID, Role: provider.RoleTool, ToolCallID: call.ID, Content: output, ToolRunState: provider.ToolRunCompleted})
		}
	}
	appendMessage(provider.Message{ID: "final", Role: provider.RoleAssistant, Content: "FINAL-ANSWER"})
	appendEvents("turn-end", session.Event{Kind: "turn/end", Payload: json.RawMessage(`{"status":"completed"}`)})

	snapshot, err := service.Query().CaptureExportSnapshot(t.Context(), runtime.Ref())
	if err != nil {
		t.Fatal(err)
	}
	appendMessage(provider.Message{ID: "later", Role: provider.RoleUser, Content: "MUST-NOT-APPEAR"})
	dir := t.TempDir()
	doc, err := Build(t.Context(), service.Query(), snapshot, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Records != 150 {
		t.Fatalf("records=%d", doc.Records)
	}
	file, err := os.Open(filepath.Join(dir, "json"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var data struct {
		MCP      map[string]int `json:"mcpList"`
		Items    []Item         `json:"items"`
		Metadata map[string]any `json:"exportMetadata"`
	}
	if err = json.NewDecoder(file).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if data.MCP["sharedHost"] != 1 || data.MCP["networkCalls"] != 1 {
		t.Fatalf("lost full-history MCP attribution: %+v", data.MCP)
	}
	count := 0
	for _, item := range data.Items {
		if text(item, "text") == "FINAL-ANSWER" && (item["samplingCount"] != float64(15) || item["toolCount"] != float64(23) || item["turnFinal"] != true) {
			t.Fatalf("lost incident telemetry: %+v", item)
		}
		if text(item, "kind") == "tool" {
			count++
			if text(item, "output") != output || text(item, "status") != "done" {
				t.Fatal("tool output/state lost")
			}
		}
	}
	if count != 23 {
		t.Fatalf("tools=%d", count)
	}
	md, err := os.ReadFile(filepath.Join(dir, "markdown"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"QUESTION-000", "FINAL-ANSWER", output, "```````"} {
		if !strings.Contains(string(md), expected) {
			t.Fatal("missing markdown content")
		}
	}
	if strings.Contains(string(md), "MUST-NOT-APPEAR") {
		t.Fatal("snapshot leaked later message")
	}
}
