package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// TestReadFileStreamsLargeGB18030 proves GB18030 content far past the 256KB
// detection sample still decodes correctly via the streaming read path.
func TestReadFileStreamsLargeGB18030(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.gbk")
	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		sb.WriteString("第一行中文 line one 你好世界\n")
	}
	sb.WriteString("终点标记 THE-END\n")
	enc, err := simplifiedchinese.GB18030.NewEncoder().String(sb.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(enc), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"path": path, "offset": 19999, "limit": 2})
	out, err := readFile{}.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "终点标记 THE-END") || !strings.Contains(out, "你好世界") {
		t.Fatalf("deep GB18030 content not decoded correctly:\n%s", out)
	}
}

// TestReadFileLargeBoundedMemory guards against re-slurping the whole file: a
// small read of a large file must allocate far less than the file size.
func TestReadFileLargeBoundedMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	var sb strings.Builder
	for i := 0; i < 130000; i++ { // ~8 MB, no NUL
		sb.WriteString("a line of perfectly ordinary text in a large utf-8 file\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"path": path, "limit": 5})

	runtime.GC()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	out, err := readFile{}.Execute(context.Background(), args)
	runtime.ReadMemStats(&m1)
	if err != nil {
		t.Fatal(err)
	}
	if alloc := m1.TotalAlloc - m0.TotalAlloc; alloc > 4<<20 {
		t.Fatalf("read allocated %d bytes for a 5-line read of an ~8MB file — slurp regression", alloc)
	}
	if !strings.Contains(out, "1→a line") {
		t.Fatalf("unexpected output: %q", out[:min(80, len(out))])
	}
}
