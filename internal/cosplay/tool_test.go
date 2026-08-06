package cosplay

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCodeVerifyToolEndToEnd: the registered tool verifies real Go code — a
// correct candidate reports full pass, a broken one reports the failure.
func TestCodeVerifyToolEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	tool := NewCodeVerifyTool()

	args, _ := json.Marshal(map[string]interface{}{
		"language": "go",
		"code":     "func double(x int) int { return x * 2 }",
		"task":     "double a number",
		"function": "double",
		"examples": []map[string]string{{"input": "21", "expected": "42"}},
	})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "tests passed") || !strings.Contains(out, "100%") {
		t.Errorf("expected a fully passing report, got: %s", out)
	}

	badArgs, _ := json.Marshal(map[string]interface{}{
		"language": "go",
		"code":     "func double(x int) int { return x }",
		"task":     "double a number",
		"function": "double",
		"examples": []map[string]string{{"input": "21", "expected": "42"}},
	})
	out2, err := tool.Execute(context.Background(), badArgs)
	if err != nil {
		t.Fatalf("execute broken: %v", err)
	}
	if strings.Contains(out2, "PASS") {
		t.Errorf("broken candidate must not report pass: %s", out2)
	}
}

// TestCodeVerifyToolShape: metadata and read-only contract.
func TestCodeVerifyToolShape(t *testing.T) {
	tool := NewCodeVerifyTool()
	if tool.Name() != "code_verify" {
		t.Errorf("name = %q", tool.Name())
	}
	if !tool.ReadOnly() {
		t.Error("code_verify must be read-only")
	}
	if len(tool.Schema()) == 0 || len(tool.Description()) == 0 {
		t.Error("schema/description must be non-empty")
	}
	if _, err := tool.Execute(context.Background(), nil); err == nil {
		t.Error("empty args (no language) must error")
	}
}

// TestCodeVerifyToolFileInput: file-based code loading works.
func TestCodeVerifyToolFileInput(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/impl.go"
	if err := os.WriteFile(path, []byte("package p\nfunc double(x int) int { return x * 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewCodeVerifyTool()
	args, _ := json.Marshal(map[string]interface{}{
		"language": "go",
		"file":     path,
		"task":     "double",
		"function": "double",
	})
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("execute from file: %v", err)
	}
	if !strings.Contains(out, "tests passed") {
		t.Errorf("unexpected output: %s", out)
	}
}
