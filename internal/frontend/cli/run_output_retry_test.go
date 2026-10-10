package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestEventsJSONLRetryRecordCarriesTheTypedClass(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputEventsJSONL)
	sink.Emit(event.Event{
		Kind: event.Retrying, RetryAttempt: 2, RetryMax: 10, RetryScope: event.RetryScopeHeaders,
		RetryCause: provider.RetryCauseUpstreamStatus, RetryStatus: 429, RetryDelayMs: 2100,
	})
	var rec map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err == nil && r["kind"] == "retrying" {
			rec = r
		}
	}
	if rec == nil {
		t.Fatalf("no retrying record in %q", out.String())
	}
	if rec["retry_cause"] != "upstream_status" || rec["retry_status"] != float64(429) || rec["retry_delay_ms"] != float64(2100) {
		t.Fatalf("record = %v, want cause/status/delay carried", rec)
	}
}
