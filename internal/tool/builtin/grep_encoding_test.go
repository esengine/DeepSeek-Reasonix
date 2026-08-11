package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	fileenc "reasonix/internal/fileutil/encoding"
)

// TestGrepUTF8ChinesePeekBoundary reproduces the misdetection where the first
// 8 KiB sample ends mid multi-byte sequence: a truncated UTF-8 tail is nearly
// always "valid" GB18030, so fileenc.Detect tagged the file GB18030 and grep
// decoded it into mojibake (Chinese patterns stopped matching).
func TestGrepUTF8ChinesePeekBoundary(t *testing.T) {
	line := strings.Repeat("中", 100) + "\n" // 301 bytes
	var sb strings.Builder
	for i := 0; i < 27; i++ {
		sb.WriteString(line) // 8127 bytes
	}
	// 8192-byte boundary lands mid "中" (E4 B8 …), a truncated UTF-8 tail.
	sb.WriteString(strings.Repeat("中", 21))
	sb.WriteString("中后续目标行统一战线内容\n")
	content := sb.String()
	if !utf8.ValidString(content) {
		t.Fatal("test fixture must be valid UTF-8")
	}
	if utf8.ValidString(content[:8*1024]) {
		t.Fatal("test fixture must end the 8 KiB sample mid UTF-8 sequence")
	}

	path := filepath.Join(t.TempDir(), "cn.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runTool(t, grepTool{}, map[string]any{"pattern": "统一战线", "path": path})
	if !strings.Contains(out, "统一战线") {
		t.Fatalf("grep on UTF-8 Chinese file (peek boundary cut) missed the pattern:\n%s", out)
	}
}

// TestGrepGB18030LongLineAfterASCIIHeader ensures making a UTF-8 sample
// character-safe does not throw away the non-ASCII evidence needed to retain
// GB18030 detection. The 8-byte header keeps the 8 KiB sample on a complete
// two-byte GB18030 character boundary.
func TestGrepGB18030LongLineAfterASCIIHeader(t *testing.T) {
	content := "header!\n" + strings.Repeat("中", 5000) + "统一战线目标\n"
	raw := fileenc.Encode(content, fileenc.GB18030)
	if utf8.Valid(raw) {
		t.Fatal("test fixture must not be valid UTF-8")
	}

	path := filepath.Join(t.TempDir(), "long-gb18030.md")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	out := runTool(t, grepTool{}, map[string]any{"pattern": "统一战线", "path": path})
	if !strings.Contains(out, "统一战线") {
		t.Fatalf("grep misdetected GB18030 after an ASCII-only header:\n%s", out)
	}
}

// TestGrepUTF8ChineseLongSingleLine covers the same boundary cut in a file
// with no newline in the sample, where trimming to '\n' is not possible.
func TestGrepUTF8ChineseLongSingleLine(t *testing.T) {
	content := strings.Repeat("中", 2730) + "统一战线目标" + strings.Repeat("中", 7000)
	path := filepath.Join(t.TempDir(), "long.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runTool(t, grepTool{}, map[string]any{"pattern": "统一战线", "path": path})
	if !strings.Contains(out, "统一战线") {
		t.Fatalf("grep on long single-line UTF-8 Chinese file missed the pattern:\n%s", out)
	}
}
