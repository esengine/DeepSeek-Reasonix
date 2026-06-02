//go:build e2e

package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"reasonix/internal/tool"
)

// TestE2EGBKRoundTrip exercises the full read → edit → write → verify cycle
// on a real GBK file written by an external tool (Python's codec), not by
// Go's encoding package. This catches encoding-table mismatches that unit
// tests with Go-generated GBK might miss.
//
// Run: go test -tags e2e ./internal/tool/builtin/ -run TestE2EGBKRoundTrip -v
func TestE2EGBKRoundTrip(t *testing.T) {
	path := os.Getenv("GBK_TEST_FILE")
	if path == "" {
		path = "/tmp/test_gbk.txt"
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("GBK test file not found at %s — create with: python3 -c \"open('$path','wb').write('你好世界\\n第二行\\n'.encode('gbk'))\"", path)
	}

	readTL, ok := tool.LookupBuiltin("read_file")
	if !ok {
		t.Fatal("read_file not registered")
	}
	editTL, ok := tool.LookupBuiltin("edit_file")
	if !ok {
		t.Fatal("edit_file not registered")
	}
	grepTL, ok := tool.LookupBuiltin("grep")
	if !ok {
		t.Fatal("grep not registered")
	}

	args := func(m map[string]any) json.RawMessage {
		b, _ := json.Marshal(m)
		return json.RawMessage(b)
	}

	// Step 1: Read the GBK file — model should see decoded UTF-8.
	t.Log("Step 1: read_file on GBK file")
	out, err := readTL.Execute(context.Background(), args(map[string]any{"path": path}))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	t.Logf("read_file output:\n%s", out)
	if !strings.Contains(out, "你好世界") || !strings.Contains(out, "第二行") {
		t.Error("read_file did not decode GBK to readable Chinese text")
	}

	// Step 2: File must still be GBK on disk (read_file is read-only).
	t.Log("Step 2: verify file still GBK after read")
	raw, _ := os.ReadFile(path)
	if utf8.Valid(raw) {
		t.Error("read_file converted GBK file to UTF-8 on disk")
	}

	// Step 3: Edit the GBK file — replace "第二行" with "新的行".
	t.Log("Step 3: edit_file on GBK file")
	editOut, err := editTL.Execute(context.Background(), args(map[string]any{
		"path":       path,
		"old_string": "第二行",
		"new_string": "新的行",
	}))
	if err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	t.Logf("edit_file: %s", editOut)

	// Step 4: File must still be GBK, with the edit applied.
	t.Log("Step 4: verify encoding and content after edit")
	raw2, _ := os.ReadFile(path)
	if utf8.Valid(raw2) {
		t.Error("edit_file converted GBK file to UTF-8 on disk")
	}
	decoded, _, _ := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw2)
	s := string(decoded)
	if !strings.Contains(s, "新的行") {
		t.Errorf("edit not applied: %q", s)
	}
	if strings.Contains(s, "第二行") {
		t.Errorf("old text still present: %q", s)
	}

	// Step 5: Read the edited file — model should see the updated UTF-8.
	t.Log("Step 5: read edited GBK file")
	out2, err := readTL.Execute(context.Background(), args(map[string]any{"path": path}))
	if err != nil {
		t.Fatalf("read_file after edit: %v", err)
	}
	if !strings.Contains(out2, "新的行") {
		t.Errorf("read_file after edit missing new text:\n%s", out2)
	}

	// Step 6: grep on the GBK file.
	t.Log("Step 6: grep on GBK file")
	grepOut, err := grepTL.Execute(context.Background(), args(map[string]any{
		"pattern": "函数",
		"path":    path,
	}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(grepOut, "函数") {
		t.Errorf("grep did not find match in decoded GBK: %s", grepOut)
	}

	// Restore original file for re-runs.
	original, _ := simplifiedchinese.GB18030.NewEncoder().String("你好世界\n这是第二行\n包含函数的测试\n")
	os.WriteFile(path, []byte(original), 0o644)
}

// Suppress unused import.
var _ = bytes.Compare
