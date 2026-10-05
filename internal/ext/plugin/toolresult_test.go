package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every content type a server may return reaches the model as something, and
// a result that carries only structuredContent is not read as an empty answer.
func TestParseToolResultKeepsEveryContentType(t *testing.T) {
	res := json.RawMessage(`{"content":[
		{"type":"text","text":"found 2 files. "},
		{"type":"resource_link","uri":"file:///repo/a.go","name":"a.go","title":"Handler"},
		{"type":"resource","resource":{"uri":"file:///repo/b.txt","mimeType":"text/plain","text":"b body"}},
		{"type":"resource","resource":{"uri":"file:///repo/c.bin","mimeType":"application/octet-stream","blob":"AAAA"}},
		{"type":"audio","data":"AAAA","mimeType":"audio/wav"}
	]}`)
	text, _, err := parseToolResult(res)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"found 2 files.",
		"[resource: Handler <file:///repo/a.go>]",
		"[resource <file:///repo/b.txt>]\nb body",
		"[resource: <file:///repo/c.bin> application/octet-stream, binary, not shown]",
		"[audio: audio/wav, not shown]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}

func TestParseToolResultFallsBackToStructuredContent(t *testing.T) {
	text, _, err := parseToolResult(json.RawMessage(`{"content":[],"structuredContent":{ "temperature": 22.5, "unit": "C" }}`))
	if err != nil || text != `{"temperature":22.5,"unit":"C"}` {
		t.Fatalf("text = %q, err %v; want the structured content as compact JSON", text, err)
	}
	// A server that also wrote the text block is read from the text, as before.
	text, _, _ = parseToolResult(json.RawMessage(`{"content":[{"type":"text","text":"22.5 C"}],"structuredContent":{"temperature":22.5}}`))
	if text != "22.5 C" {
		t.Fatalf("text = %q, want the text block", text)
	}
}
