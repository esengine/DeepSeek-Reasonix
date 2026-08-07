package cosplay

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLimitedBufferCapsOutput(t *testing.T) {
	var b limitedBuffer
	b.max = 16
	big := strings.Repeat("x", 100)
	n, err := b.Write([]byte(big))
	if err != nil {
		t.Fatal(err)
	}
	if n != 100 {
		t.Errorf("Write returned %d, want full length 100 (over-cap bytes are dropped, not an error)", n)
	}
	if b.buf.Len() != 16 {
		t.Errorf("captured %d bytes, want 16", b.buf.Len())
	}
	// Further writes are dropped entirely but still report full length.
	n, err = b.Write([]byte("more"))
	if err != nil || n != 4 {
		t.Errorf("Write after cap = (%d, %v), want (4, nil)", n, err)
	}
	if b.buf.Len() != 16 {
		t.Errorf("after overflow captured %d bytes, want still 16", b.buf.Len())
	}
}

func TestLimitedBufferPartialFill(t *testing.T) {
	var b limitedBuffer
	b.max = 10
	if _, err := b.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("world!")); err != nil { // 11 bytes, cap 10
		t.Fatal(err)
	}
	if got := b.buf.String(); got != "helloworld" {
		t.Errorf("partial fill = %q, want %q", got, "helloworld")
	}
}

func TestLimitedBufferConcurrentWrites(t *testing.T) {
	var b limitedBuffer
	b.max = 1 << 20
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, _ = b.Write([]byte(fmt.Sprintf("g%d-%d ", seed, j)))
			}
		}(i)
	}
	wg.Wait()
	if b.buf.Len() == 0 {
		t.Fatal("concurrent writes produced no output")
	}
	// Run under -race this test exercises the stdout/stderr copier pattern.
}

// TestRunBoundedDropsOverflowWithoutFailing verifies that a program emitting
// more than maxCaptureBytes is not misjudged as failed (the old short-write
// bug turned over-cap output into io.ErrShortWrite → test failure).
func TestRunBoundedDropsOverflowWithoutFailing(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "main.py")
	// Emit 2 MiB of noise then the PASS marker — over cap, but a valid smoke.
	src := "import sys\nsys.stdout.write('x' * (2 * 1024 * 1024))\nprint('PASS')\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := runBounded(ctx, "python", path)
	if err != nil {
		t.Fatalf("over-cap run must not error: %v", err)
	}
	if len(out) > maxCaptureBytes+64 {
		t.Errorf("captured %d bytes, want <= %d", len(out), maxCaptureBytes)
	}
}
