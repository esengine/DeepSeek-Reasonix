package jobs

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

func TestJobNilPanicCannotPublishSuccess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"0", "1"} {
		t.Run("panicnil="+mode, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestJobNilPanicChild$", "-test.count=1", "-test.v")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "GODEBUG=") && !strings.HasPrefix(entry, "REASONIX_JOB_NIL_PANIC=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "GODEBUG=panicnil="+mode, "REASONIX_JOB_NIL_PANIC=1")
			out, err := cmd.CombinedOutput()
			t.Logf("explicit panicnil=%s child:\n%s", mode, out)
			if err != nil {
				t.Errorf("compiled job probe failed: %v", err)
			}
		})
	}
}

func TestJobNilPanicChild(t *testing.T) {
	if os.Getenv("REASONIX_JOB_NIL_PANIC") != "1" {
		return
	}
	for _, phase := range []string{"run", "finalizer", "uninvoked_finalizer"} {
		t.Run(phase, func(t *testing.T) {
			runJobPanicCase(t, phase, nil)
		})
	}
}

func TestJobPanicPreservesErrorCause(t *testing.T) {
	for _, phase := range []string{"run", "finalizer", "uninvoked_finalizer"} {
		t.Run(phase, func(t *testing.T) {
			runJobPanicCase(t, phase, errors.New("host callback cause"))
		})
	}
}

func runJobPanicCase(t *testing.T, phase string, panicValue any) {
	t.Helper()
	manager := NewManager(event.Discard)
	defer manager.Close()
	if phase == "uninvoked_finalizer" {
		manager.Close()
	}
	recorder := &finalizationRecorder{done: make(chan error, 1)}
	manager.SetTaskRecorder(recorder)
	var siblings atomic.Int32
	job := manager.StartForSession("session", "task", "panic result", func(context.Context, io.Writer) (string, error) {
		if phase == "run" {
			panic(panicValue)
		}
		return "done", nil
	}, WithFinalizer(func(Finalization) error {
		if phase != "run" {
			panic(panicValue)
		}
		return nil
	}), WithFinalizer(func(Finalization) error {
		siblings.Add(1)
		return nil
	}))
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not complete")
	}
	cause := <-recorder.done
	if cause == nil {
		t.Error("panic became a successful completion")
	}
	if value, ok := panicValue.(error); ok && !errors.Is(cause, value) {
		t.Errorf("panic error identity lost: %v", cause)
	}
	if phase != "run" && !errors.Is(cause, ErrFinalization) {
		t.Errorf("cleanup panic lost ErrFinalization: %v", cause)
	}
	job.mu.Lock()
	status := job.status
	job.mu.Unlock()
	want := Failed
	if phase == "uninvoked_finalizer" {
		want = Interrupted
		if !errors.Is(cause, context.Canceled) {
			t.Errorf("no-launch cause lost: %v", cause)
		}
	}
	if status != want {
		t.Errorf("status=%s want %s", status, want)
	}
	if siblings.Load() != 1 {
		t.Errorf("sibling cleanup count=%d", siblings.Load())
	}
	if repeated := job.finalize(Finalization{}); phase != "run" && !errors.Is(repeated, ErrFinalization) {
		t.Errorf("repeated finalization lost cleanup failure: %v", repeated)
	}
	if siblings.Load() != 1 {
		t.Error("repeated finalization repeated sibling cleanup")
	}
}
