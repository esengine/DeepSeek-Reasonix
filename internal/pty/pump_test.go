package pty

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPTYMassiveOutputCompletion(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)
	defer mgr.CloseAll()

	sess, err := mgr.Start(context.Background(), StartOptions{
		ID:  "massive-output-sess",
		Cwd: tmpDir,
	})
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	// Output ~200,000 bytes (5000 lines), exceeding standard RingBuffer single-read/peek limits (128KB)
	res, err := sess.RunCommand(context.Background(), "seq 1 5000", 5*time.Second)
	if err != nil {
		t.Fatalf("RunCommand on massive output failed: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("expected StatusCompleted, got: %s", res.Status)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got: %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "5000") {
		t.Fatalf("expected output to contain final sequence number 5000")
	}
}

func TestPTYConcurrentReadDoesNotStealCompletion(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)
	defer mgr.CloseAll()

	sess, err := mgr.Start(context.Background(), StartOptions{
		ID:  "concurrent-read-sess",
		Cwd: tmpDir,
	})
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Spin up 5 goroutines constantly draining the User RingBuffer via Read()
	var readWg sync.WaitGroup
	for i := 0; i < 5; i++ {
		readWg.Add(1)
		go func() {
			defer readWg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_ = sess.Read(4096)
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
	}

	// Execute command via RunCommand while concurrent reads are draining the user ring buffer
	res, err := sess.RunCommand(context.Background(), "echo CHUNK_A && sleep 0.1 && echo CHUNK_B", 3*time.Second)
	cancel()
	readWg.Wait()

	if err != nil {
		t.Fatalf("RunCommand failed during concurrent reads: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("expected StatusCompleted despite concurrent reads, got: %s", res.Status)
	}
	if !strings.Contains(res.Output, "CHUNK_A") || !strings.Contains(res.Output, "CHUNK_B") {
		t.Fatalf("RunCommand output was incomplete or stolen: %q", res.Output)
	}
}

func TestPTYManagerConcurrentGetOrCreate(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir)
	defer mgr.CloseAll()

	const concurrency = 10
	var wg sync.WaitGroup
	errs := make([]error, concurrency)
	sessions := make([]*Session, concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := mgr.GetOrCreate(context.Background(), "same-shared-session", tmpDir)
			sessions[idx] = s
			errs[idx] = err
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d failed GetOrCreate: %v", i, err)
		}
	}

	// Verify all returned the exact same Session instance
	first := sessions[0]
	for i := 1; i < concurrency; i++ {
		if sessions[i] != first {
			t.Fatalf("goroutine %d got a different session instance: %v vs %v", i, sessions[i], first)
		}
	}

	if list := mgr.List(); len(list) != 1 {
		t.Fatalf("expected exactly 1 session in manager, got: %d", len(list))
	}
}
