package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWorkspaceScanGoroutinesBoundedByReaders(t *testing.T) {
	root := t.TempDir()
	for i := range 1024 {
		path := filepath.Join(root, fmt.Sprintf("d%04d", i), "a", "b", "c")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "file"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	baseline := runtime.NumGoroutine()
	done := make(chan workspaceScan, 1)
	go func() { done <- scanWorkspaceTo(t.Context(), root, 2048) }()
	peak := baseline
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case scan := <-done:
			if !scan.complete || len(scan.state) != 1024 {
				t.Fatalf("complete = %v, files = %d", scan.complete, len(scan.state))
			}
			if peak-baseline > scanReaders+2 {
				t.Fatalf("peak goroutine growth = %d, reader limit = %d", peak-baseline, scanReaders)
			}
			return
		case <-tick.C:
			peak = max(peak, runtime.NumGoroutine())
		}
	}
}
