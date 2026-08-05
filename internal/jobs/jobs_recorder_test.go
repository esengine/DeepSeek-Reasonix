package jobs

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
)

// recordingRecorder captures lifecycle calls for assertions.
type recordingRecorder struct {
	mu     sync.Mutex
	starts []string
	dones  []string
	status []Status
}

func (r *recordingRecorder) RecordStart(id, kind, label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, id+"|"+kind+"|"+label)
}

func (r *recordingRecorder) RecordDone(id string, st Status, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dones = append(r.dones, id)
	r.status = append(r.status, st)
}

func (r *recordingRecorder) snapshot() (starts, dones []string, status []Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.starts...), append([]string(nil), r.dones...), append([]Status(nil), r.status...)
}

func TestTaskRecorderHook_StartAndDone(t *testing.T) {
	rec := &recordingRecorder{}
	m := NewManager(event.Discard, WithTaskRecorder(rec))
	defer m.Close()

	j := m.Start("task", "demo", func(ctx context.Context, out io.Writer) (string, error) {
		return "answer", nil
	})
	if res := m.Wait(context.Background(), []string{j.ID}, 5); len(res) != 1 || res[0].Status != Done {
		t.Fatalf("wait = %+v", res)
	}

	starts, dones, status := rec.snapshot()
	wantStart := j.ID + "|task|demo"
	if len(starts) != 1 || starts[0] != wantStart {
		t.Fatalf("starts = %v, want [%s]", starts, wantStart)
	}
	if len(dones) != 1 || dones[0] != j.ID || len(status) != 1 || status[0] != Done {
		t.Fatalf("dones = %v status = %v, want [%s] [done]", dones, status, j.ID)
	}
}

func TestTaskRecorderHook_Failed(t *testing.T) {
	rec := &recordingRecorder{}
	m := NewManager(event.Discard, WithTaskRecorder(rec))
	defer m.Close()

	j := m.Start("bash", "", func(ctx context.Context, out io.Writer) (string, error) {
		return "", context.DeadlineExceeded
	})
	if res := m.Wait(context.Background(), []string{j.ID}, 5); len(res) != 1 || res[0].Status != Failed {
		t.Fatalf("wait = %+v", res)
	}

	_, dones, status := rec.snapshot()
	if len(dones) != 1 || status[0] != Failed {
		t.Fatalf("dones = %v status = %v, want [%s] [failed]", dones, status, j.ID)
	}
}

func TestTaskRecorderHook_Killed(t *testing.T) {
	rec := &recordingRecorder{}
	m := NewManager(event.Discard, WithTaskRecorder(rec))
	defer m.Close()

	block := make(chan struct{})
	j := m.Start("task", "", func(ctx context.Context, out io.Writer) (string, error) {
		<-block // hang until killed
		return "", nil
	})
	time.Sleep(50 * time.Millisecond)
	m.Kill(j.ID)
	close(block)
	m.Wait(context.Background(), []string{j.ID}, 5)

	_, dones, status := rec.snapshot()
	if len(dones) != 1 || status[0] != Killed {
		t.Fatalf("dones = %v status = %v, want [%s] [killed]", dones, status, j.ID)
	}
}

func TestTaskRecorderHook_SetAfterConstruction(t *testing.T) {
	rec := &recordingRecorder{}
	m := NewManager(event.Discard)
	m.SetTaskRecorder(rec)
	defer m.Close()

	j := m.Start("bash", "echo", func(ctx context.Context, out io.Writer) (string, error) {
		return "", nil
	})
	m.Wait(context.Background(), []string{j.ID}, 5)

	starts, _, _ := rec.snapshot()
	if len(starts) != 1 {
		t.Fatalf("starts = %v, want 1 call", starts)
	}
}

func TestTaskRecorderHook_NilRecorderIsNoop(t *testing.T) {
	m := NewManager(event.Discard, WithTaskRecorder(nil))
	defer m.Close()

	j := m.Start("bash", "echo", func(ctx context.Context, out io.Writer) (string, error) {
		return "", nil
	})
	if res := m.Wait(context.Background(), []string{j.ID}, 5); len(res) != 1 {
		t.Fatalf("wait = %+v", res)
	}
}
