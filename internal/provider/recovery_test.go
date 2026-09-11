package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManagedRecoveryOneHTTPRequestAndServerDelay(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limit"}}`))
	}))
	defer server.Close()
	ctx := WithManagedRecovery(context.Background())
	_, err := SendWithRetry(ctx, server.Client(), SendOptions{}, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	})
	f := ClassifyRecovery(err)
	if calls != 1 || !f.Retryable || f.RetryAfter != 2*time.Minute || f.Code != "rate_limit" {
		t.Fatalf("calls=%d failure=%+v err=%v", calls, f, err)
	}
}
func TestRecoveryDoesNotRetryPermanentOrUnknownErrors(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("arbitrary failure"), &APIError{Status: 429, Body: `{"error":{"code":"insufficient_quota"}}`}, &APIError{Status: 503, ShouldRetry: "false"}, &AuthError{Status: 401}} {
		if ClassifyRecovery(err).Retryable {
			t.Fatalf("retried %v", err)
		}
	}
}

func TestRecoveryWaitExhaustedIsTerminalAndDiagnosable(t *testing.T) {
	cause := &APIError{Provider: "p", Status: 503, Body: `{"error":{"code":"overloaded"}}`, TraceID: "trace_1"}
	err := fmt.Errorf("run: %w", &RecoveryWaitExhaustedError{Phase: "headers", Code: "overloaded", Status: 503, Waited: 10 * time.Minute, Attempts: 13, Cause: cause})
	if f := ClassifyRecovery(err); f.Retryable || f.Phase != "headers" || f.Status != 503 || f.Code != "overloaded" {
		t.Fatalf("failure=%+v", f)
	}
	if d := DiagnoseFailure(err); d.Kind != "recovery_wait_exhausted" || d.Status != 503 || d.TraceID != "trace_1" {
		t.Fatalf("diagnostic=%+v", d)
	}
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "provider unreachable for 10m0s (headers): p: status 503") {
		t.Fatalf("err=%v", err)
	}
	if AsRecoveryWaitExhausted(cause) != nil || AsRecoveryWaitExhausted(nil) != nil {
		t.Fatal("plain failures must not read as an exhausted wait")
	}
}

func TestOpaqueGoBadRequestDoesNotGuessReplayFailure(t *testing.T) {
	// Observed from Go's custom DeepSeek Anthropic route after an invalid
	// replay. The same opaque body cannot establish the cause for real users.
	err := &APIError{Provider: "opencode-go", Status: 400, Body: `{"model":"deepseek-v4-flash"}`}
	if AsReasoningReplayError(err) != nil || ClassifyRecovery(err).Retryable {
		t.Fatal("opaque 400 must not trigger guessed history repair or automatic regeneration")
	}
}
func TestWriteEvidenceNeverEntersModelMessages(t *testing.T) {
	original := []Message{{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "x", Name: "write_file", Arguments: `{}`, WriteIntents: []json.RawMessage{json.RawMessage(`{"version":99,"future":"retain"}`)}}}}}
	projected := ModelMessages(original)
	if len(projected[0].ToolCalls[0].WriteIntents) != 0 || len(original[0].ToolCalls[0].WriteIntents) != 1 {
		t.Fatal("projection leaked or mutated evidence")
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []Message
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded[0].ToolCalls[0].WriteIntents[0]) != string(original[0].ToolCalls[0].WriteIntents[0]) {
		t.Fatal("unknown version lost")
	}
}
