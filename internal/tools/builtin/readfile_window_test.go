package builtin

import (
	"bytes"
	"fmt"
	"io"
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
