package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileenc "reasonix/internal/fileutil/encoding"
)

// TestReadFileUTF8ChineseLongSingleLine guards the 256 KiB sampling boundary
// cut: a sample with no newline ending mid multi-byte sequence misdetects as
// GB18030 (the truncated UTF-8 tail is "valid" there), and read_file would
// show the whole file as mojibake.
func TestReadFileUTF8ChineseLongSingleLine(t *testing.T) {
	content := strings.Repeat("中", 90000) // 270000 bytes, no newline, > sample
	path := filepath.Join(t.TempDir(), "long.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runTool(t, readFile{}, map[string]any{"path": path})
	if !strings.HasPrefix(out, "1→中") {
		t.Fatalf("read_file garbled a UTF-8 Chinese long single line:\n%.120s", out)
	}
}

// TestReadFileGB18030LongLineAfterASCIIHeader guards the inverse boundary:
// making the 256 KiB sample safe for UTF-8 must retain the non-ASCII bytes
// that distinguish a long GB18030 line from an ASCII-only prefix.
func TestReadFileGB18030LongLineAfterASCIIHeader(t *testing.T) {
	content := "header!\n统一战线目标" + strings.Repeat("中", 132000)
	raw := fileenc.Encode(content, fileenc.GB18030)
	path := filepath.Join(t.TempDir(), "long-gb18030.md")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	out := runTool(t, readFile{}, map[string]any{"path": path, "offset": 1, "limit": 1})
	if !strings.Contains(out, "统一战线目标") {
		t.Fatalf("read_file misdetected GB18030 after an ASCII-only header:\n%.120s", out)
	}
}
