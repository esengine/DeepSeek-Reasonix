package plugin

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseToolResultContentOnlyUnchanged(t *testing.T) {
	want := `{"datasources":[{"name":"Prometheus"}]}`
	res := `{"content":[{"type":"text","text":` + quotedJSON(want) + `}]}`

	text, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if text != want {
		t.Fatalf("text = %q, want unchanged %q", text, want)
	}
	if len(images) != 0 {
		t.Fatalf("images = %v, want none", images)
	}
}

func TestParseToolResultEquivalentStructuredJSONIsNotDuplicated(t *testing.T) {
	want := `{"folders":[{"uid":"torchbearing-demo"}],"page":{"total":1}}`
	res := `{"content":[{"type":"text","text":` + quotedJSON(want) + `}],` +
		`"structuredContent":{"page":{"total":1},"folders":[{"uid":"torchbearing-demo"}]}}`

	text, _, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if text != want {
		t.Fatalf("text = %q, want one unchanged JSON value %q", text, want)
	}
	if strings.Count(text, "torchbearing-demo") != 1 {
		t.Fatalf("equivalent structured content was duplicated: %q", text)
	}
}

func TestParseToolResultAppendsStructuredJSONAfterGenericText(t *testing.T) {
	res := `{"content":[{"type":"text","text":"Dagu read completed."}],` +
		`"structuredContent":{"target":"dags","data":{"items":[{"name":"hello-playbook"}]}}}`

	text, _, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	want := "Dagu read completed.\n\n" +
		`{"target":"dags","data":{"items":[{"name":"hello-playbook"}]}}`
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}

func TestParseToolResultStructuredOnlySupportsJSONValues(t *testing.T) {
	tests := []struct {
		name       string
		structured string
		want       string
	}{
		{name: "object", structured: `{"healthy": true}`, want: `{"healthy":true}`},
		{name: "array", structured: `[1, 2, 3]`, want: `[1,2,3]`},
		{name: "string", structured: `"ready"`, want: `"ready"`},
		{name: "number", structured: `42`, want: `42`},
		{name: "boolean", structured: `true`, want: `true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := `{"content":[],"structuredContent":` + tt.structured + `}`
			text, _, err := parseToolResult(json.RawMessage(res))
			if err != nil {
				t.Fatalf("parseToolResult: %v", err)
			}
			if text != tt.want {
				t.Fatalf("text = %q, want %q", text, tt.want)
			}
		})
	}
}

func TestParseToolResultNullStructuredContentIsIgnored(t *testing.T) {
	res := `{"content":[{"type":"text","text":"unchanged"}],"structuredContent":null}`
	text, _, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if text != "unchanged" {
		t.Fatalf("text = %q, want unchanged", text)
	}
}

func TestParseToolResultStructuredErrorRemainsAnError(t *testing.T) {
	res := `{"content":[{"type":"text","text":"Dagu read failed."}],` +
		`"structuredContent":{"code":"not_found","name":"missing-dag"},"isError":true}`

	text, _, err := parseToolResult(json.RawMessage(res))
	if err == nil {
		t.Fatal("want tool error")
	}
	for _, want := range []string{"Dagu read failed.", `"code":"not_found"`, `"name":"missing-dag"`} {
		if !strings.Contains(text, want) || !strings.Contains(err.Error(), want) {
			t.Fatalf("structured error missing %q: text=%q err=%v", want, text, err)
		}
	}
}

func TestParseToolResultPreservesImagesWhenAppendingStructuredJSON(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	res := `{"content":[{"type":"text","text":"before "},` + imageItem("image/png", payload) +
		`],"structuredContent":{"caption":"chart"}}`

	text, images, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if want := "before [image: image/png]\n\n{\"caption\":\"chart\"}"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if len(images) != 1 || images[0] != "data:image/png;base64,"+payload {
		t.Fatalf("images = %v, want original image", images)
	}
}

func TestParseToolResultNaturalLanguageDoesNotSuppressStructuredJSON(t *testing.T) {
	res := `{"content":[{"type":"text","text":"folders: torchbearing-demo"}],` +
		`"structuredContent":{"folders":["torchbearing-demo"]}}`

	text, _, err := parseToolResult(json.RawMessage(res))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if !strings.Contains(text, "folders: torchbearing-demo\n\n") ||
		!strings.Contains(text, `{"folders":["torchbearing-demo"]}`) {
		t.Fatalf("structured JSON was not appended: %q", text)
	}
}

func TestParseToolResultRejectsMalformedEnvelope(t *testing.T) {
	if _, _, err := parseToolResult(json.RawMessage(`{"content":[],"structuredContent":`)); err == nil {
		t.Fatal("want malformed tool result error")
	}
}

func quotedJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
