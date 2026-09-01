package builtin

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// TestScanWindowedReadDoesNotConsumeWholeFile guards the regression where scan()
// drained the entire file (a second `for scanner.Scan()` loop) just to count the
// remaining lines for the pagination trailer. A small windowed read of a large
// file must read only a small prefix, not all of it.
func TestScanWindowedReadDoesNotConsumeWholeFile(t *testing.T) {
	var buf bytes.Buffer
	for i := 1; i <= 100_000; i++ {
		fmt.Fprintf(&buf, "line %d\n", i)
	}
	total := buf.Len()
	if total < 500*1024 {
		t.Fatalf("test fixture too small (%d bytes) to be meaningful", total)
	}

	cr := &countingReader{r: bytes.NewReader(buf.Bytes())}
	out, err := readFile{}.scan(cr, 0, 3)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(out, "1→line 1") || !strings.Contains(out, "3→line 3") {
		t.Fatalf("window content wrong:\n%s", out)
	}
	if strings.Contains(out, "line 4") {
		t.Fatalf("window leaked line 4:\n%s", out)
	}
	if !strings.Contains(out, "more lines below") {
		t.Fatalf("pagination trailer missing:\n%s", out)
	}
	if cr.n > 100*1024 {
		t.Fatalf("read %d of %d bytes for a 3-line window; should read only a small prefix", cr.n, total)
	}
	t.Logf("read %d of %d bytes (%.1f%%) for a 3-line window", cr.n, total, 100*float64(cr.n)/float64(total))
}

func TestReadFileReusesLineOffsetCacheForRepeatedPagination(t *testing.T) {
	oldCache := readFileLineStarts
	readFileLineStarts = newReadFileLineStartCache(2)
	t.Cleanup(func() { readFileLineStarts = oldCache })

	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	var buf bytes.Buffer
	for i := 1; i <= 20_000; i++ {
		fmt.Fprintf(&buf, "line %05d payload payload payload payload\n", i)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	const firstLimit = 10_000
	first := runTool(t, readFile{}, map[string]any{"path": path, "offset": 0, "limit": firstLimit})
	if !strings.Contains(first, "10000→line 10000") {
		t.Fatalf("first page did not include line 10000:\n%s", first)
	}
	line, byteOffset, ok := readFileLineStarts.nearest(path, info, firstLimit)
	if !ok || line != firstLimit || byteOffset <= 0 {
		t.Fatalf("cache did not retain line %d start after first page: line=%d byte=%d ok=%v", firstLimit+1, line, byteOffset, ok)
	}

	second := runTool(t, readFile{}, map[string]any{"path": path, "offset": firstLimit, "limit": 2})
	if !strings.Contains(second, "10001→line 10001") || !strings.Contains(second, "10002→line 10002") {
		t.Fatalf("second page read wrong window:\n%s", second)
	}
	line, byteOffset, ok = readFileLineStarts.nearest(path, info, firstLimit+2)
	if !ok || line < firstLimit+2 || byteOffset <= 0 {
		t.Fatalf("cache did not advance after second page: line=%d byte=%d ok=%v", line, byteOffset, ok)
	}
}
