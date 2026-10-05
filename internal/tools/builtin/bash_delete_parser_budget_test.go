package builtin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/safety/sandbox"
)

type fakeParser struct {
	starts, kills atomic.Int32
}

// install makes every helper answer its first request after firstDelay and
// later ones after warmDelay.
func (f *fakeParser) install(t *testing.T, firstDelay, warmDelay time.Duration) {
	t.Helper()
	prev := startDeleteParserFunc
	t.Cleanup(func() { startDeleteParserFunc = prev })
	startDeleteParserFunc = func(sandbox.Shell) (*deleteParserProcess, error) {
		f.starts.Add(1)
		inR, inW := io.Pipe()
		outR, outW := io.Pipe()
		go func() {
			defer outW.Close()
			r := bufio.NewReader(inR)
			delay := firstDelay
			for {
				if _, err := r.ReadBytes('\n'); err != nil {
					f.kills.Add(1)
					return
				}
				time.Sleep(delay)
				delay = warmDelay
				if _, err := outW.Write([]byte("{\"standalone\":true,\"calls\":[]}\n")); err != nil {
					return
				}
			}
		}()
		return &deleteParserProcess{input: inW, output: bufio.NewReader(outR)}, nil
	}
}

func setBudgets(t *testing.T, warm, cold time.Duration) {
	t.Helper()
	w, c := deleteParserWarmBudget, deleteParserColdBudget
	t.Cleanup(func() { deleteParserWarmBudget, deleteParserColdBudget = w, c })
	deleteParserWarmBudget, deleteParserColdBudget = warm, cold
}

var fakeShellSeq atomic.Int32

func fakeShell(t *testing.T) sandbox.Shell {
	return sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: fmt.Sprintf("fake-%s-%d", t.Name(), fakeShellSeq.Add(1))}
}

func TestDeleteParserColdStartExceedingWarmBudget(t *testing.T) {
	setBudgets(t, 100*time.Millisecond, 3*time.Second)
	var f fakeParser
	f.install(t, 400*time.Millisecond, 0)
	if _, err := analyzePowerShellDelete(context.Background(), fakeShell(t), "x"); err != nil {
		t.Fatalf("cold start within the cold budget refused: %v", err)
	}
}

func TestDeleteParserWarmCallKeepsWarmBudget(t *testing.T) {
	setBudgets(t, 150*time.Millisecond, 3*time.Second)
	var f fakeParser
	f.install(t, 0, 600*time.Millisecond)
	sh := fakeShell(t)
	if _, err := analyzePowerShellDelete(context.Background(), sh, "x"); err != nil {
		t.Fatal(err)
	}
	_, err := analyzePowerShellDelete(context.Background(), sh, "x")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("warm call was not bounded by the warm budget: %v", err)
	}
}

func TestDeleteParserTimeoutKillsHelper(t *testing.T) {
	setBudgets(t, 100*time.Millisecond, 150*time.Millisecond)
	var f fakeParser
	f.install(t, time.Second, 0)
	_, err := analyzePowerShellDelete(context.Background(), fakeShell(t), "x")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cold timeout did not refuse: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.kills.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.kills.Load() != 1 {
		t.Fatal("timed-out helper was not stopped")
	}
}

func TestDeleteParserParallelCallsShareOneColdStart(t *testing.T) {
	setBudgets(t, 100*time.Millisecond, 3*time.Second)
	var f fakeParser
	f.install(t, 500*time.Millisecond, 0)
	sh := fakeShell(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			_, err := analyzePowerShellDelete(context.Background(), sh, "x")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("parallel call refused during cold start: %v", err)
		}
	}
	if f.starts.Load() != 1 {
		t.Fatalf("helper started %d times", f.starts.Load())
	}
}
