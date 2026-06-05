package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestDoctorCommandPrintsJSON(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := doctorCommand([]string{"--json"}, "test-version"); rc != 0 {
			t.Fatalf("doctorCommand rc = %d, want 0", rc)
		}
	})

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("doctor --json output is not JSON: %v\n%s", err, out)
	}
	if decoded["version"] != "test-version" {
		t.Fatalf("version = %v, want test-version", decoded["version"])
	}
}

func TestRunDispatchesDoctor(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := Run([]string{"doctor"}, "dispatch-version"); rc != 0 {
			t.Fatalf("Run doctor rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "reasonix dispatch-version doctor") {
		t.Fatalf("doctor output missing header:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
