package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFailureDiagnosticOpaqueAndSafe(t *testing.T) {
	for _, body := range []string{"", `{}`, `{"model":"anything"}`} {
		d := DiagnoseFailure(&APIError{Status: 400, Body: body, TraceID: "trace_123"})
		if d.Kind != "upstream_reason_missing" || d.Status != 400 || d.TraceID != "trace_123" {
			t.Fatalf("diagnostic=%+v", d)
		}
	}
	e := &APIError{Status: 400, Body: `{"error":{"message":"invalid temperature"},"request":{"reasoning":"private"}}`, TraceID: "https://billing.invalid/private"}
	d := DiagnoseFailure(e)
	b, _ := json.Marshal(d)
	if d.Kind != "request" || strings.Contains(string(b), "private") || strings.Contains(string(b), "billing") {
		t.Fatalf("unsafe diagnostic: %s", b)
	}
	if DiagnoseFailure(nil) != nil {
		t.Fatal("nil error diagnostic")
	}
}
func TestSearchStatusStaysOutsideReplay(t *testing.T) {
	raw := json.RawMessage(`{"type":"web_search_call","id":"s","status":"completed","opaque":"proof"}`)
	call := ServerSearchCall{ID: "s", Raw: raw}
	merged := MergeServerSearch(nil, call)
	if merged[0].SourcesStatus != SourcesNotProvided {
		t.Fatal(merged)
	}
	original := []Message{{Role: RoleAssistant, ServerSearch: merged}}
	model := ModelMessages(original)
	if model[0].ServerSearch[0].SourcesStatus != "" || string(model[0].ServerSearch[0].Raw) != string(raw) {
		t.Fatal("presentation contaminated replay")
	}
	if original[0].ServerSearch[0].SourcesStatus != SourcesNotProvided || ProjectionMessages(original)[0].ServerSearch[0].SourcesStatus != SourcesNotProvided {
		t.Fatal("lost stored display status")
	}
	if ServerSearchSourcesStatus(ServerSearchCall{}) != "" {
		t.Fatal("inferred missing from legacy record")
	}
	if ServerSearchSourcesStatus(ServerSearchCall{Raw: json.RawMessage(`{"type":"web_search_tool_result_error"}`)}) != "" {
		t.Fatal("error classified as search success")
	}
	if ServerSearchSourcesStatus(ServerSearchCall{Results: []ServerSearchHit{{URL: "https://example.com"}}}) != SourcesAvailable {
		t.Fatal("valid source missing")
	}
}
