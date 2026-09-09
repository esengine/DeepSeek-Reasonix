package provider

import (
	"encoding/json"
	"errors"
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

func TestFailureDiagnosticKeepsDisplayAndStableIdentitySeparate(t *testing.T) {
	err := &APIError{Provider: "deepseek-anthropic", ProviderDisplayName: "Deepseek2", Protocol: "openai", Status: 404, RequestPath: "/anthropic/v1/chat/completions"}
	d := DiagnoseFailure(err)
	if d.ProviderID != "deepseek-anthropic" || d.ProviderDisplayName != "Deepseek2" || d.Protocol != "openai" || d.Status != 404 || d.RequestPath != "/anthropic/v1/chat/completions" {
		t.Fatalf("diagnostic = %+v", d)
	}
	if got := err.Error(); got != "Deepseek2 · Chat Completions: status 404" {
		t.Fatalf("display error = %q", got)
	}
}

func TestRequestFailureKeepsDisplayAndStableIdentitySeparate(t *testing.T) {
	cause := errors.New("invalid request URL")
	err := &RequestFailure{
		Identity:  RequestIdentity{Provider: "deepseek-anthropic", DisplayName: "Deepseek2", Protocol: "openai"},
		Operation: "build request",
		Err:       cause,
	}
	d := DiagnoseFailure(err)
	if d.ProviderID != "deepseek-anthropic" || d.ProviderDisplayName != "Deepseek2" || d.Protocol != "openai" {
		t.Fatalf("diagnostic identity = %+v", d)
	}
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "deepseek-anthropic") || !strings.Contains(err.Error(), "Deepseek2 · Chat Completions") {
		t.Fatalf("request error did not preserve cause and display identity: %v", err)
	}
}

func TestFailureDiagnosticDetailUsesOnlySafeOperatorFields(t *testing.T) {
	diagnostic := &FailureDiagnostic{
		ProviderID:          "deepseek-anthropic",
		ProviderDisplayName: "Deepseek2",
		Protocol:            "openai",
		RequestPath:         "/anthropic/v1/chat/completions",
		TraceID:             "trace-secret",
	}
	if got, want := FailureDiagnosticDetail(diagnostic), "Connection ID: deepseek-anthropic\nRequest path: /anthropic/v1/chat/completions"; got != want {
		t.Fatalf("FailureDiagnosticDetail() = %q, want %q", got, want)
	}
}

func TestInterruptedTurnRecoveryOptionalFieldsRemainBackwardCompatible(t *testing.T) {
	type legacyRecovery struct {
		Pending          bool     `json:"pending,omitempty"`
		InterruptedTools []string `json:"interrupted_tools,omitempty"`
	}
	current := InterruptedTurnRecovery{
		TerminalStatus: "failed", FailureDiagnostic: &FailureDiagnostic{Kind: "request", Status: 404},
		Pending: true, InterruptedTools: []string{"bash"},
	}
	raw, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	var legacy legacyRecovery
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatalf("legacy reader rejected optional fields: %v", err)
	}
	if !legacy.Pending || len(legacy.InterruptedTools) != 1 || legacy.InterruptedTools[0] != "bash" {
		t.Fatalf("legacy fields lost: %+v", legacy)
	}
	var old InterruptedTurnRecovery
	if err := json.Unmarshal([]byte(`{"pending":true,"interrupted_tools":["bash"]}`), &old); err != nil {
		t.Fatalf("current reader rejected legacy record: %v", err)
	}
	if old.TerminalStatus != "" || old.FailureDiagnostic != nil || !old.Pending {
		t.Fatalf("legacy defaults changed: %+v", old)
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
