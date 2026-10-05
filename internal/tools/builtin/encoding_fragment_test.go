package builtin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	fileenc "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

// longGBKLine is a GB18030 file whose first line outruns any detection window
// and puts an odd byte before it, so every window ends inside a character with
// no newline to cut at.
func longGBKLine(t *testing.T, chars int) string {
	t.Helper()
	gb, err := simplifiedchinese.GB18030.NewEncoder().String("x" + strings.Repeat("啊", chars) + "\n目标行\n")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(testenv.TempDir(t), "long.gbk")
	if err := os.WriteFile(path, []byte(gb), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGrepGB18030PeekWithoutNewline(t *testing.T) {
	path := longGBKLine(t, 5000)
	out := runTool(t, grepTool{}, map[string]any{"pattern": "目标", "path": path})
	if !strings.Contains(out, "目标行") {
		t.Fatalf("expected a match past the peek, got:\n%s", out)
	}
}

func TestReadFileGB18030SampleWithoutNewline(t *testing.T) {
	path := longGBKLine(t, 140000)
	readTL, _ := tool.LookupBuiltin("read_file")
	out, err := readTL.Execute(context.Background(), e2eArgs(map[string]any{"path": path, "offset": 1}))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(out, "目标行") {
		t.Fatalf("read_file did not decode GB18030 past the sample:\n%s", out)
	}
}

// An edit that adds Chinese to a CP936 file holding its 0x80 euro writes CP936:
// the file keeps one encoding, and the bytes the edit did not touch.
func TestEditKeepsCP936FileWithEuroByte(t *testing.T) {
	enc := simplifiedchinese.GBK.NewEncoder()
	before, _ := enc.String("价格 €5\nvalue := 1\n")
	path := filepath.Join(testenv.TempDir(t), "cp936.txt")
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	editTL, _ := tool.LookupBuiltin("edit_file")
	if _, err := editTL.Execute(context.Background(), e2eArgs(map[string]any{
		"path":       path,
		"old_string": "value := 1",
		"new_string": "value := 2 // 中文",
	})); err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	want, _ := enc.String("价格 €5\nvalue := 2 // 中文\n")
	if got, _ := os.ReadFile(path); !bytes.Equal(got, []byte(want)) {
		t.Fatalf("edit_file wrote % x, want % x", got, want)
	}
}

// cp936File writes a CP936 file holding its 0x80 euro and returns its path and
// bytes, for writes that must leave it untouched.
func cp936File(t *testing.T) (string, []byte) {
	t.Helper()
	before, _ := simplifiedchinese.GBK.NewEncoder().String("// 中文 €5\nvalue := 1\n")
	path := filepath.Join(testenv.TempDir(t), "cp936.go")
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, []byte(before)
}

// A write adding a character CP936 cannot hold is refused with that identity,
// and the file keeps every byte; nothing rewrites it as UTF-8.
func TestWritesRefuseCharacterCP936CannotHold(t *testing.T) {
	cases := map[string]func(path string) map[string]any{
		"edit_file": func(path string) map[string]any {
			return map[string]any{"path": path, "old_string": "value := 1", "new_string": "value := 2 // ✅ 完成"}
		},
		"multi_edit": func(path string) map[string]any {
			return map[string]any{"path": path, "edits": []map[string]any{{"old_string": "value := 1", "new_string": "value := 2 // 🚀"}}}
		},
		"write_file": func(path string) map[string]any {
			return map[string]any{"path": path, "content": "// 中文 €5\nvalue := 2 // ™\n"}
		},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			path, before := cp936File(t)
			tl, _ := tool.LookupBuiltin(name)
			_, err := tl.Execute(context.Background(), e2eArgs(args(path)))
			if !errors.Is(err, fileenc.ErrUnencodable) {
				t.Fatalf("%s err = %v, want ErrUnencodable", name, err)
			}
			if got, _ := os.ReadFile(path); !bytes.Equal(got, before) {
				t.Fatalf("%s changed the file:\n got % x\nwant % x", name, got, before)
			}
		})
	}
}
