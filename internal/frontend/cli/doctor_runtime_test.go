package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDoctorRuntimeWithoutRuntimeSaysUnknown(t *testing.T) {
	text := captureStdout(t, func() {
		if rc := doctorRuntimeCommand(nil); rc != 0 {
			t.Fatalf("doctor runtime rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(text, "runtime status: unavailable\n") {
		t.Fatalf("doctor runtime lost its unavailable status line:\n%s", text)
	}
	if !strings.Contains(text, "recoverability: unknown") {
		t.Fatalf("recoverability reads as measured without a runtime:\n%s", text)
	}
	if !strings.Contains(text, "resume: unknown") {
		t.Fatalf("resume reads as measured without a runtime:\n%s", text)
	}
	for _, claim := range []string{"clean=true", "cleanRollback=true"} {
		if strings.Contains(text, claim) {
			t.Fatalf("doctor runtime printed unobserved %s:\n%s", claim, text)
		}
	}

	raw := captureStdout(t, func() {
		if rc := doctorRuntimeCommand([]string{"--json"}); rc != 0 {
			t.Fatalf("doctor runtime --json rc = %d, want 0", rc)
		}
	})
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("doctor runtime --json output is not JSON: %v\n%s", err, raw)
	}
	for _, key := range []string{"status", "recoverability", "resume"} {
		fact, ok := decoded[key]
		if !ok {
			t.Fatalf("JSON omits %q; an unobserved fact must be explicit null:\n%s", key, raw)
		}
		if string(fact) != "null" {
			t.Fatalf("JSON %q = %s, want null when no runtime was asked:\n%s", key, fact, raw)
		}
	}
	for _, claim := range []string{`"clean": true`, `"cleanRollback": true`} {
		if strings.Contains(raw, claim) {
			t.Fatalf("JSON claims an unobserved %s verdict:\n%s", claim, raw)
		}
	}
}
