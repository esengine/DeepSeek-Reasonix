package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

func recording(saved *[]string) context.Context {
	return tool.WithResultRecorder(context.Background(), func(b json.RawMessage) (string, error) {
		*saved = append(*saved, string(b))
		return "tool-result:abc", nil
	})
}

// A text-only result needs no retained copy and reads exactly as it always did.
func TestCaptureToolResultLeavesPlainTextAlone(t *testing.T) {
	var saved []string
	text, _, err := captureToolResult(recording(&saved), json.RawMessage(`{"content":[{"type":"text","text":"plain"}]}`))
	if err != nil || text != "plain" || len(saved) != 0 {
		t.Fatalf("capture = %q, %v; retained %d copies", text, err, len(saved))
	}
}

// A rich result is retained whole, and the model gets its reference and the
// names of its structured fields rather than the structure itself.
func TestCaptureToolResultRetainsRichResultsAndNamesTheirFields(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"done"}],"structuredContent":{"url":"https://example.test/v.mp4","job_id":"j"},"future":true}`
	var saved []string
	text, _, err := captureToolResult(recording(&saved), json.RawMessage(raw))
	if err != nil || len(saved) != 1 || saved[0] != raw {
		t.Fatalf("capture = %q, %v; saved %q", text, err, saved)
	}
	for _, want := range []string{"done", "tool-result:abc", "structuredContent fields: job_id, url"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "example.test") {
		t.Fatalf("structured values reached the text: %q", text)
	}
}

func TestCaptureToolResultRefusesToRetainPastTheLimitAndSaysSo(t *testing.T) {
	big := `{"content":[{"type":"image","mimeType":"image/png","data":"` + strings.Repeat("A", tool.ResultRetainLimit) + `"}]}`
	var saved []string
	text, _, _ := captureToolResult(recording(&saved), json.RawMessage(big))
	if len(saved) != 0 || !strings.Contains(text, "not retained") {
		t.Fatalf("oversized result: retained %d, text tail %q", len(saved), text[max(0, len(text)-120):])
	}
}

func TestCaptureToolResultReportsStorageFailure(t *testing.T) {
	failure := errors.New("disk unavailable")
	ctx := tool.WithResultRecorder(context.Background(), func(json.RawMessage) (string, error) { return "", failure })
	text, _, err := captureToolResult(ctx, json.RawMessage(`{"content":[{"type":"text","text":"generated"}],"structuredContent":{"id":1}}`))
	if !strings.HasPrefix(text, "generated") || !errors.Is(err, failure) {
		t.Fatalf("capture = %q, %v", text, err)
	}
}
