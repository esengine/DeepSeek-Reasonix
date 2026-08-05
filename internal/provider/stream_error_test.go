package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestStreamDecodeErrorQuotesPayload(t *testing.T) {
	err := StreamDecodeError("openrouter", "DONE", errors.New("invalid character 'D' looking for beginning of value"))
	got := err.Error()
	if !strings.Contains(got, `payload "DONE"`) {
		t.Errorf("error should quote the offending payload: %q", got)
	}
	if !strings.Contains(got, "openrouter") {
		t.Errorf("error should name the provider: %q", got)
	}
}

func TestStreamDecodeErrorBoundsPayload(t *testing.T) {
	long := strings.Repeat("x", streamPayloadExcerpt*3)
	got := StreamDecodeError("p", long, errors.New("boom")).Error()
	if len(got) > streamPayloadExcerpt*2 {
		t.Errorf("excerpt is unbounded: %d chars", len(got))
	}
	if !strings.Contains(got, "…") {
		t.Errorf("truncated excerpt should be marked: %q", got)
	}
}

func TestStreamDecodeErrorEmptyPayload(t *testing.T) {
	got := StreamDecodeError("p", "  ", errors.New("boom")).Error()
	if !strings.Contains(got, "(empty)") {
		t.Errorf("empty payload should say so: %q", got)
	}
}
