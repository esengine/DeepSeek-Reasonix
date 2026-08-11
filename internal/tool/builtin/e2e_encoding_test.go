package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"reasonix/internal/tool"
)

func gbkBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(s))
	if err != nil {
		t.Fatalf("encode GBK: %v", err)
	}
	return b
}

func TestE2EGBKRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test_gbk.txt")
	if err := os.WriteFile(path, gbkBytes(t, "你好世界\n这是第二行\n包含函数的测试\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	readTL, _ := tool.LookupBuiltin("read_file")
	editTL, _ := tool.LookupBuiltin("edit_file")
	grepTL, _ := tool.LookupBuiltin("grep")

	args := func(m map[string]any) json.RawMessage {
		b, _ := json.Marshal(m)
		return json.RawMessage(b)
	}

	out, err := readTL.Execute(context.Background(), args(map[string]any{"path": path}))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(out, "你好世界") || !strings.Contains(out, "这是第二行") {
		t.Errorf("read_file did not decode GBK to readable Chinese:\n%s", out)
	}

	if raw, _ := os.ReadFile(path); utf8.Valid(raw) {
		t.Error("read_file rewrote GBK file as UTF-8 on disk")
	}

	if _, err := editTL.Execute(context.Background(), args(map[string]any{
		"path":       path,
		"old_string": "这是第二行",
		"new_string": "这是新的行",
	})); err != nil {
		t.Fatalf("edit_file: %v", err)
	}

	raw2, _ := os.ReadFile(path)
	if utf8.Valid(raw2) {
		t.Error("edit_file rewrote GBK file as UTF-8 on disk")
	}
	decoded, _, _ := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw2)
	if s := string(decoded); !strings.Contains(s, "这是新的行") || strings.Contains(s, "这是第二行") {
		t.Errorf("edit not applied to GBK file on disk: %q", s)
	}

	out2, err := readTL.Execute(context.Background(), args(map[string]any{"path": path}))
	if err != nil {
		t.Fatalf("read_file after edit: %v", err)
	}
	if !strings.Contains(out2, "这是新的行") {
		t.Errorf("read_file after edit missing new text:\n%s", out2)
	}

	grepOut, err := grepTL.Execute(context.Background(), args(map[string]any{
		"pattern": "函数",
		"path":    path,
	}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(grepOut, "函数") {
		t.Errorf("grep did not match decoded GBK content:\n%s", grepOut)
	}
}

func e2eArgs(m map[string]any) json.RawMessage {
	b, _ := json.Marshal(m)
	return json.RawMessage(b)
}

// write_file must preserve the encoding of a file it overwrites rather than
// always writing UTF-8, which would silently corrupt a GBK file.
func TestE2EWriteFilePreservesGBKOnOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gbk.txt")
	if err := os.WriteFile(path, gbkBytes(t, "原始内容\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTL, _ := tool.LookupBuiltin("write_file")
	if _, err := writeTL.Execute(context.Background(), e2eArgs(map[string]any{
		"path":    path,
		"content": "全新中文内容\n第二行\n",
	})); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if utf8.Valid(raw) {
		t.Error("write_file rewrote an existing GBK file as UTF-8 on disk")
	}
	decoded, _, _ := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw)
	if s := string(decoded); !strings.Contains(s, "全新中文内容") || !strings.Contains(s, "第二行") {
		t.Errorf("write_file content wrong after GBK round-trip: %q", s)
	}
}

// A newly created file (no existing encoding to preserve) defaults to UTF-8.
func TestE2EWriteFileNewFileIsUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.txt")
	writeTL, _ := tool.LookupBuiltin("write_file")
	if _, err := writeTL.Execute(context.Background(), e2eArgs(map[string]any{
		"path":    path,
		"content": "新文件中文\n",
	})); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !utf8.Valid(raw) {
		t.Error("a newly created file should be written as UTF-8")
	}
	if string(raw) != "新文件中文\n" {
		t.Errorf("new file content = %q, want UTF-8 Chinese", raw)
	}
}

// delete_range must decode a GBK file before matching anchors (a UTF-8 anchor
// would never match the GBK bytes otherwise) and re-encode it on write.
func TestE2EDeleteRangePreservesGBK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gbk.txt")
	if err := os.WriteFile(path, gbkBytes(t, "第一行\n第二行\n第三行\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	delTL, _ := tool.LookupBuiltin("delete_range")
	if _, err := delTL.Execute(context.Background(), e2eArgs(map[string]any{
		"path":         path,
		"start_anchor": "第二行",
		"end_anchor":   "第二行",
	})); err != nil {
		t.Fatalf("delete_range on GBK file: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if utf8.Valid(raw) {
		t.Error("delete_range rewrote a GBK file as UTF-8 on disk")
	}
	decoded, _, _ := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw)
	s := string(decoded)
	if strings.Contains(s, "第二行") {
		t.Errorf("delete_range did not remove the target line: %q", s)
	}
	if !strings.Contains(s, "第一行") || !strings.Contains(s, "第三行") {
		t.Errorf("delete_range removed the wrong lines: %q", s)
	}
}

// grep classifies a file from its 8 KiB peek alone. CJK characters are three
// bytes wide, so that window usually stops mid-character; treating the cut as
// "not UTF-8" sent the file down the GB18030 branch and produced mojibake.
func TestE2EGrepUTF8CJKLargerThanPeek(t *testing.T) {
	dir := t.TempDir()
	raw := cjkFixtureSplitAt(t, grepBinaryPeek, 12*1024,
		"统一战线的形成过程\n", "这是一段中文正文内容用于填充文件长度\n", "尾部中文行 tail-marker-1931\n")
	if err := os.WriteFile(filepath.Join(dir, "chapter.md"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	grepTL, _ := tool.LookupBuiltin("grep")
	out, err := grepTL.Execute(context.Background(), e2eArgs(map[string]any{
		"pattern": "统一战线",
		"path":    dir,
	}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "统一战线的形成过程") {
		t.Errorf("grep missed a Chinese pattern in a UTF-8 file:\n%s", out)
	}

	// An ASCII pattern matched before the fix too, but printed the line as
	// mojibake — the Chinese around it must survive the round trip.
	out, err = grepTL.Execute(context.Background(), e2eArgs(map[string]any{
		"pattern": "tail-marker-1931",
		"path":    dir,
	}))
	if err != nil {
		t.Fatalf("grep ascii pattern: %v", err)
	}
	if !strings.Contains(out, "尾部中文行") {
		t.Errorf("grep mangled Chinese text on a matched line:\n%s", out)
	}
}

// Trimming the detection window must not cost grep its GB18030 support.
func TestE2EGrepGBKLargerThanPeek(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("统一战线的形成过程\n")
	for b.Len() < 12*1024 {
		b.WriteString("这是一段中文正文内容用于填充文件长度\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "gbk.txt"), gbkBytes(t, b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	grepTL, _ := tool.LookupBuiltin("grep")
	out, err := grepTL.Execute(context.Background(), e2eArgs(map[string]any{
		"pattern": "统一战线",
		"path":    dir,
	}))
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "统一战线的形成过程") {
		t.Errorf("grep missed a Chinese pattern in a GBK file:\n%s", out)
	}
}

// read_file bounds its detection sample at a newline, which leaves a file with
// no newline in that window (single-line CJK export) cut mid-character.
func TestE2EReadFileSingleLineCJKLargerThanSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oneline.txt")
	raw := cjkFixtureSplitAt(t, readFileDetectSample, readFileDetectSample+8*1024,
		"统一战线", "这是一段中文正文内容没有任何换行符", "")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	readTL, _ := tool.LookupBuiltin("read_file")
	out, err := readTL.Execute(context.Background(), e2eArgs(map[string]any{"path": path}))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(out, "统一战线") {
		t.Errorf("read_file mangled a single-line UTF-8 CJK file:\n%.200s", out)
	}
}

// cjkFixtureSplitAt builds a UTF-8 Chinese fixture of at least minSize bytes
// whose byte at boundary falls inside a multi-byte character. ASCII padding
// shifts the boundary a byte at a time, so one of three widths always splits
// a 3-byte character.
func cjkFixtureSplitAt(t *testing.T, boundary, minSize int, head, filler, tail string) []byte {
	t.Helper()
	for pad := range 3 {
		var b strings.Builder
		b.WriteString(strings.Repeat("x", pad))
		b.WriteString(head)
		for b.Len() < minSize {
			b.WriteString(filler)
		}
		b.WriteString(tail)
		raw := []byte(b.String())
		if !utf8.Valid(raw) {
			t.Fatal("fixture must be valid UTF-8")
		}
		if raw[boundary]&0xC0 == 0x80 {
			return raw
		}
	}
	t.Fatal("no fixture width split a character at the detection boundary")
	return nil
}
