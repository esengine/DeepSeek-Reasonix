package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	// Other agent lifetimes can start background work while this scan runs.
	stop := make(chan struct{})
	var noise sync.WaitGroup
	defer func() { close(stop); noise.Wait() }()
	for range scanReaders * 2 {
		noise.Go(func() { <-stop })
	}
	done := make(chan workspaceScan, 1)
	go func() { done <- scanWorkspaceTo(t.Context(), root, 2048) }()
	peak := 0
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case scan := <-done:
			if !scan.complete || len(scan.state) != 1024 {
				t.Fatalf("complete = %v, files = %d", scan.complete, len(scan.state))
			}
			if peak > scanReaders {
				t.Fatalf("peak scan goroutines = %d, reader limit = %d", peak, scanReaders)
			}
			return
		case <-tick.C:
			peak = max(peak, workspaceScanGoroutines())
		}
	}
}

func workspaceScanGoroutines() int {
	var stacks []runtime.StackRecord
	for {
		n, ok := runtime.GoroutineProfile(stacks)
		if ok {
			stacks = stacks[:n]
			break
		}
		stacks = make([]runtime.StackRecord, n+32)
	}
	count := 0
	for _, stack := range stacks {
		frames := runtime.CallersFrames(stack.Stack())
		for {
			frame, more := frames.Next()
			if strings.HasPrefix(frame.Function, "reasonix/internal/runtime/agent.scanWorkspaceTo") || frame.Function == "reasonix/internal/runtime/agent.(*scanWalk).dir" {
				count++
				break
			}
			if !more {
				break
			}
		}
	}
	return count
}
