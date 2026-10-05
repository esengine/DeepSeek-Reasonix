package builtin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

// The file tools are how a Windows session changes a file without PowerShell's
// ANSI/UTF-16 defaults, so each one must write back the charset and BOM it found.
func TestFileToolsKeepEncodingAndBOM(t *testing.T) {
	const before, after = "标题：旧的说明\r\n第二行\r\n", "标题：新的说明\r\n第二行\r\n"
	bom := []byte{0xEF, 0xBB, 0xBF}
	encodings := []struct {
		name   string
		encode func(string) []byte
	}{
		{"gbk", func(s string) []byte { return gbkBytes(t, s) }},
		{"utf8", func(s string) []byte { return []byte(s) }},
		{"utf8-bom", func(s string) []byte { return append(append([]byte{}, bom...), s...) }},
	}
	ops := []struct {
		name string
		args func(path string) map[string]any
	}{
		{"edit_file", func(path string) map[string]any {
			return map[string]any{"path": path, "old_string": "旧的", "new_string": "新的"}
		}},
		{"multi_edit", func(path string) map[string]any {
			return map[string]any{"path": path, "edits": []map[string]any{{"old_string": "旧的", "new_string": "新的"}}}
		}},
		{"write_file", func(path string) map[string]any {
			return map[string]any{"path": path, "content": after}
		}},
	}
	for _, enc := range encodings {
		for _, op := range ops {
			t.Run(enc.name+"/"+op.name, func(t *testing.T) {
				path := filepath.Join(testenv.TempDir(t), "f.txt")
				if err := os.WriteFile(path, enc.encode(before), 0o644); err != nil {
					t.Fatal(err)
				}
				tl, ok := tool.LookupBuiltin(op.name)
				if !ok {
					t.Fatalf("%s not registered", op.name)
				}
				if _, err := tl.Execute(context.Background(), e2eArgs(op.args(path))); err != nil {
					t.Fatalf("%s: %v", op.name, err)
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if want := enc.encode(after); !bytes.Equal(got, want) {
					t.Fatalf("on disk % x\nwant       % x", got, want)
				}
			})
		}
	}
}
