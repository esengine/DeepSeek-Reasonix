package agent

import (
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type noticeRecorder struct {
	mu  sync.Mutex
	got []event.Event
}

func (r *noticeRecorder) Emit(e event.Event) {
	if e.Kind != event.Notice || e.Code != event.NoticeCodeCompactHeld {
		return
	}
	r.mu.Lock()
	r.got = append(r.got, e)
	r.mu.Unlock()
}

func (r *noticeRecorder) held() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.got...)
}

func TestHoldBelowTheTriggerIsNotAnnounced(t *testing.T) {
	rec := &noticeRecorder{}
	a := New(&failingSummaryProvider{}, tool.NewRegistry(), &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
	}}, Options{ContextWindow: 20_000, CompactRatio: 0.5, WorkspaceID: "workspace", ModelRef: "model"}, rec)
	a.BindSessionPath(testenv.TempDir(t)+"/session.jsonl", true)
	a.window().recordContextMaintenanceOutcome("", CompactionTriggerPressure, "summary", "blocked", NoopFixedPrefixAboveTrigger, "too large")
	_ = prepare(t, a, CompactionTriggerPressure)
	if got := rec.held(); len(got) != 0 {
		t.Fatalf("a hold with nothing due was announced: %+v", got)
	}
}
