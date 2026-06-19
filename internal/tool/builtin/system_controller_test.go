package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSystemControl_Name(t *testing.T) {
	if got := (systemController{}).Name(); got != "system_control" {
		t.Fatalf("Name() = %q, want system_control", got)
	}
}

func TestSystemControl_SchemaIsValidJSON(t *testing.T) {
	var v any
	if err := json.Unmarshal((systemController{}).Schema(), &v); err != nil {
		t.Fatalf("Schema() is not valid JSON: %v", err)
	}
}

func TestSystemControl_ReadOnly(t *testing.T) {
	if rc := (systemController{}).ReadOnly(); rc {
		t.Fatal("ReadOnly() should be false (system_control has side effects)")
	}
}

// ---------------------------------------------------------------------------
// open_url — unit tests
// ---------------------------------------------------------------------------

func TestOpenURL_RejectsEmptyURL(t *testing.T) {
	_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
		"action": "open_url",
		"url":    "",
	}))
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected url-required error, got %v", err)
	}
}

func TestOpenURL_RejectsDangerousScheme(t *testing.T) {
	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd", "vbscript:msgbox"} {
		_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
			"action": "open_url",
			"url":    bad,
		}))
		if err == nil || !strings.Contains(err.Error(), "dangerous") {
			t.Fatalf("expected dangerous-scheme error for %q, got %v", bad, err)
		}
	}
}

func TestOpenURL_AcceptsValidURL(t *testing.T) {
	_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
		"action": "open_url",
		"url":    "https://example.com",
	}))
	if err != nil {
		t.Fatalf("expected no error for valid URL, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// run_command — unit tests
// ---------------------------------------------------------------------------

func TestRunCommand_RejectsEmptyCmd(t *testing.T) {
	_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
		"action": "run_command",
		"cmd":    "",
	}))
	if err == nil || !strings.Contains(err.Error(), "cmd is required") {
		t.Fatalf("expected cmd-required error, got %v", err)
	}
}

func TestRunCommand_RejectsDangerousPatterns(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"notepad.exe && rm -rf /"},
		{"notepad.exe || calc.exe"},
		{"calc.exe | dir"},
		{"notepad ; del /f /q ."},
		{"notepad `id`"},
		{"calc.exe $(whoami)"},
		{"notepad.exe > /dev/null"},
	}
	for _, tt := range tests {
		_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
			"action": "run_command",
			"cmd":    tt.cmd,
		}))
		if err == nil || !strings.Contains(err.Error(), "dangerous") {
			t.Fatalf("expected dangerous-pattern error for %q, got %v", tt.cmd, err)
		}
	}
}

func TestRunCommand_RejectsNonWhitelisted(t *testing.T) {
	tests := []string{"regedit.exe", "shutdown", "powershell", "python", "bash", "cmd.exe"}
	for _, exe := range tests {
		_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
			"action": "run_command",
			"cmd":    exe,
		}))
		if err == nil || !strings.Contains(err.Error(), "not in the allowed list") {
			t.Fatalf("expected not-allowed error for %q, got %v", exe, err)
		}
	}
}

func TestRunCommand_AcceptsWhitelistedKnownApp(t *testing.T) {
	// Find at least one whitelisted app that exists on this system.
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{"notepad.exe", "calc.exe"}
	} else {
		candidates = []string{"echo"} // not in whitelist; just for reference
		candidates = []string{}        // on non-Windows we can't easily test a match
	}
	if len(candidates) == 0 {
		t.Skip("no whitelisted apps to test on this platform")
	}
	_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
		"action": "run_command",
		"cmd":    candidates[0],
	}))
	// Don't fail on error — the app might not be installed in CI — just check
	// that the error is about LookPath, not about the whitelist.
	if err != nil {
		if strings.Contains(err.Error(), "not in the allowed list") {
			t.Fatalf("%q should be in the allowed list: %v", candidates[0], err)
		}
		// Allowed but not found on PATH — acceptable in constrained CI.
		t.Logf("whitelisted app %q not found on PATH (%v); this is fine", candidates[0], err)
	}
}

// ---------------------------------------------------------------------------
// create_file — unit tests
// ---------------------------------------------------------------------------

func TestCreateFile_RejectsEmptyPath(t *testing.T) {
	_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
		"action":  "create_file",
		"path":    "",
		"content": "hello",
	}))
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected path-required error, got %v", err)
	}
}

func TestCreateFile_RejectsExistingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl := systemController{roots: []string{dir}}
	_, err := ctrl.Execute(context.Background(), testJSON(t, map[string]any{
		"action":  "create_file",
		"path":    p,
		"content": "overwrite attempt",
	}))
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected overwrite rejection, got %v", err)
	}
}

func TestCreateFile_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "new.txt")
	ctrl := systemController{roots: []string{dir}}
	_, err := ctrl.Execute(context.Background(), testJSON(t, map[string]any{
		"action":  "create_file",
		"path":    p,
		"content": "content here",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content here" {
		t.Fatalf("content = %q, want %q", string(data), "content here")
	}
}

func TestCreateFile_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "deep.txt")
	ctrl := systemController{roots: []string{dir}}
	_, err := ctrl.Execute(context.Background(), testJSON(t, map[string]any{
		"action":  "create_file",
		"path":    p,
		"content": "nested",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Fatal("nested file was not created")
	}
}

func TestCreateFile_RespectsWorkDir(t *testing.T) {
	dir := t.TempDir()
	ctrl := systemController{roots: []string{dir}, workDir: dir}
	_, err := ctrl.Execute(context.Background(), testJSON(t, map[string]any{
		"action":  "create_file",
		"path":    "relative.txt",
		"content": "relative path test",
	}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "relative.txt")); os.IsNotExist(err) {
		t.Fatal("relative file was not created")
	}
}

func TestCreateFile_ConfineRejectsOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "..", "escape.txt")
	ctrl := systemController{roots: []string{dir}}
	_, err := ctrl.Execute(context.Background(), testJSON(t, map[string]any{
		"action":  "create_file",
		"path":    outside,
		"content": "escaped",
	}))
	if err == nil || !strings.Contains(err.Error(), "outside the writable roots") {
		t.Fatalf("expected confinement error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// unknown action
// ---------------------------------------------------------------------------

func TestSystemControl_RejectsUnknownAction(t *testing.T) {
	_, err := (systemController{}).Execute(context.Background(), testJSON(t, map[string]any{
		"action": "nuke_system",
	}))
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected unknown-action error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func testJSON(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
