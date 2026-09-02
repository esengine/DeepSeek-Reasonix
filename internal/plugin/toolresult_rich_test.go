package plugin

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseToolResultProjectsStructuredContentDeterministically(t *testing.T) {
	res := json.RawMessage(`{
		"content":[{"type":"text","text":"summary"}],
		"structuredContent":{"z":2,"a":{"second":2,"first":1}},
		"_meta":{"private":"must-not-reach-model"}
	}`)

	text, images, err := parseToolResult(res)
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	want := "summary\n[MCP structured content]\n{\n  \"a\": {\n    \"first\": 1,\n    \"second\": 2\n  },\n  \"z\": 2\n}"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want none", images)
	}
	if strings.Contains(text, "must-not-reach-model") {
		t.Fatal("CallToolResult _meta leaked into the model projection")
	}

	again, _, err := parseToolResult(res)
	if err != nil || again != text {
		t.Fatalf("projection is not deterministic: text=%q again=%q err=%v", text, again, err)
	}
}

func TestParseToolResultProjectsResourceLinkMetadataOnly(t *testing.T) {
	res := json.RawMessage(`{"content":[{
		"type":"resource_link",
		"uri":"https://example.test/report.json",
		"name":"report",
		"title":"Quarterly report",
		"description":"A compact report",
		"mimeType":"application/json",
		"size":42,
		"_meta":{"token":"secret"}
	}]}`)

	text, images, err := parseToolResult(res)
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	want := `[MCP resource link] {"uri":"https://example.test/report.json","name":"report","title":"Quarterly report","description":"A compact report","mimeType":"application/json","size":42}`
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want none", images)
	}
	if strings.Contains(text, "secret") {
		t.Fatal("resource _meta leaked into the model projection")
	}
}

func TestParseToolResultProjectsEmbeddedResources(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	res := json.RawMessage(`{"content":[
		{"type":"resource","resource":{"uri":"file:///notes.txt","mimeType":"text/plain","text":"hello from resource","_meta":{"private":"hidden"}}},
		{"type":"resource","resource":{"uri":"file:///chart.png","mimeType":"image/png","blob":"` + payload + `"}},
		{"type":"resource","resource":{"uri":"file:///archive.bin","mimeType":"application/octet-stream","blob":"AQID"}}
	]}`)

	text, images, err := parseToolResult(res)
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	for _, want := range []string{
		`[MCP embedded resource] {"uri":"file:///notes.txt","mimeType":"text/plain"}`,
		"hello from resource",
		`[MCP embedded resource] {"uri":"file:///chart.png","mimeType":"image/png"}`,
		"[image: image/png]",
		`[MCP binary resource] {"mimeType":"application/octet-stream","encodedBytes":4,"decodedBytes":3,"data":"omitted"}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text = %q, want substring %q", text, want)
		}
	}
	if strings.Contains(text, payload) || strings.Contains(text, "hidden") {
		t.Fatalf("embedded base64 or _meta leaked into text: %q", text)
	}
	if len(images) != 1 || images[0] != "data:image/png;base64,"+payload {
		t.Fatalf("images = %v, want embedded png", images)
	}
}

func TestParseToolResultProjectsAudioAsBoundedMetadata(t *testing.T) {
	res := json.RawMessage(`{"content":[{"type":"audio","mimeType":"audio/wav","data":"AQID","_meta":{"private":"hidden"}}]}`)

	text, images, err := parseToolResult(res)
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	want := `[MCP audio] {"mimeType":"audio/wav","encodedBytes":4,"decodedBytes":3,"data":"omitted: no audio provider channel"}`
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if strings.Contains(text, "AQID") || strings.Contains(text, "hidden") {
		t.Fatalf("audio base64 or _meta leaked into text: %q", text)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want none", images)
	}
}

func TestParseToolResultUnknownContentIsVisibleWithoutInlinePayload(t *testing.T) {
	res := json.RawMessage(`{"content":[{"type":"video","mimeType":"video/mp4","data":"private-inline-payload","_meta":{"private":"hidden"}}]}`)

	text, images, err := parseToolResult(res)
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if text != `[unsupported MCP content block] {"type":"video"}` {
		t.Fatalf("text = %q", text)
	}
	if strings.Contains(text, "private-inline-payload") || strings.Contains(text, "hidden") {
		t.Fatalf("unknown inline payload or _meta leaked into text: %q", text)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want none", images)
	}
}

func TestParseToolResultRichProjectionIsBounded(t *testing.T) {
	structured, err := json.Marshal(map[string]any{"payload": strings.Repeat("s", maxToolResultRichItemBytes+1)})
	if err != nil {
		t.Fatal(err)
	}
	embeddedText := strings.Repeat("r", maxToolResultRichItemBytes+1)
	res := json.RawMessage(`{"content":[{"type":"resource","resource":{"uri":"file:///large.txt","mimeType":"text/plain","text":` + string(mustJSON(t, embeddedText)) + `}}],"structuredContent":` + string(structured) + `}`)

	text, images, err := parseToolResult(res)
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if len(text) > maxToolResultRichProjectionBytes {
		t.Fatalf("projection = %d bytes, want at most %d", len(text), maxToolResultRichProjectionBytes)
	}
	if !strings.Contains(text, "projection truncated") {
		t.Fatalf("text = %q, want embedded-resource truncation marker", text)
	}
	if !strings.Contains(text, "structured content omitted") {
		t.Fatalf("text = %q, want structured-content omission marker", text)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want none", images)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
