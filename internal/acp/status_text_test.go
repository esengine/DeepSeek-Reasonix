package acp

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipStatusValuePreservesUTF8AcrossJSON(t *testing.T) {
	const limit = 2_048
	want := strings.Repeat("x", limit-1)
	clipped := clipStatusValue(want+"中", limit)

	if clipped != want {
		t.Fatalf("clipStatusValue() = %q, want the complete UTF-8 prefix", clipped)
	}
	if len(clipped) > limit {
		t.Fatalf("clipStatusValue() returned %d bytes, limit %d", len(clipped), limit)
	}
	if !utf8.ValidString(clipped) {
		t.Fatalf("clipStatusValue() returned invalid UTF-8: %q", clipped)
	}

	wire, err := json.Marshal(clipped)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded string
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded != clipped {
		t.Fatalf("JSON round trip changed clipped diagnostic: got %q, want %q", decoded, clipped)
	}
}

func TestClipStatusValueRepairsInvalidUTF8(t *testing.T) {
	clipped := clipStatusValue("status: \xff", 32)
	if !utf8.ValidString(clipped) {
		t.Fatalf("clipStatusValue() returned invalid UTF-8: %q", clipped)
	}
	if clipped != "status: \uFFFD" {
		t.Fatalf("clipStatusValue() = %q, want invalid bytes replaced", clipped)
	}
}
