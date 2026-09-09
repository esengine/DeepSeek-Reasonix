package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamScannerDistinguishesTruncationFromMalformedEvents(t *testing.T) {
	for _, tc := range []struct {
		name, line  string
		interrupted bool
	}{
		{"cut string", `data: {"delta":"partial`, true},
		{"cut literal", `data: {"delta":nu`, true},
		{"malformed complete line", "data: {\"delta\":nu\n", false},
		{"malformed complete CRLF", "data: {\"delta\":nu\r\n", false},
		{"malformed earlier field", `data: {oops}`, false},
		{"html gateway reply", "data: <html>bad gateway</html>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStreamScanner(strings.NewReader("event: delta\n"+tc.line), 1024*1024)
			for range 2 {
				if !s.Scan() {
					t.Fatal("missing lines")
				}
			}
			payload := strings.TrimSpace(strings.TrimPrefix(s.Text(), "data:"))
			var v any
			err := json.Unmarshal([]byte(payload), &v)
			if err == nil {
				t.Fatal("fixture must fail decode")
			}
			got := s.DecodeError("fixture", payload, err)
			if IsStreamInterrupted(got) != tc.interrupted {
				t.Fatalf("interrupted=%v error=%v", IsStreamInterrupted(got), got)
			}
			if tc.interrupted && !ClassifyRecovery(got).Retryable {
				t.Fatal("cut stream is not retryable")
			}
		})
	}
}

func TestStreamScannerCompleteTerminalWithoutNewlineStillDecodes(t *testing.T) {
	s := NewStreamScanner(strings.NewReader(`data: {"finish_reason":"stop"}`), 1024*1024)
	if !s.Scan() {
		t.Fatal("missing final line")
	}
	var value any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(s.Text(), "data: ")), &value); err != nil {
		t.Fatal(err)
	}
	if s.Scan() || s.Err() != nil {
		t.Fatal("unexpected extra line or error")
	}
}
