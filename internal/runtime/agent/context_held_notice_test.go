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

func TestHeldRetryAfterFailedSummaryIsAnnouncedOncePerTurn(t *testing.T) {
	a := newPressureAgent(t, &failingSummaryProvider{})
	rec := &noticeRecorder{}
	a.svc.sink = rec
	_ = prepare(t, a, CompactionTriggerPressure)
	if got := rec.held(); len(got) != 0 {
		t.Fatalf("the attempt itself announced a hold: %+v", got)
	}
	_ = prepare(t, a, CompactionTriggerPressure)
	_ = prepare(t, a, CompactionTriggerPressure)
	got := rec.held()
	if len(got) != 1 {
		t.Fatalf("held notices = %d, want 1 for the turn", len(got))
	}
	a.activeTurnCreatedAt.Store(a.activeTurnCreatedAt.Load() + 1)
	_ = prepare(t, a, CompactionTriggerPressure)
	if n := len(rec.held()); n != 2 {
		t.Fatalf("held notices after a new turn = %d, want the cause announced again (2)", n)
	}
	if got[0].Detail != string(FailSummaryFailed) || got[0].Level != event.LevelWarn || got[0].Text == "" {
		t.Fatalf("held notice = %+v, want warn level, English fallback and the failure code as detail", got[0])
	}
}

func TestHoldBelowTheTriggerIsNotAnnounced(t *testing.T) {
	rec := &noticeRecorder{}
	a := New(&failingSummaryProvider{}, tool.NewRegistry(), &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
	}}, Options{ContextWindow: 20_000, CompactRatio: 0.5, WorkspaceID: "workspace", ModelRef: "model"}, rec)
	a.BindSessionPath(testenv.TempDir(t)+"/session.jsonl", true)
	a.window().recordContextMaintenanceOutcome("", CompactionTriggerPressure, "summary", "blocked", FailSummaryInputTooLarge, "too large")
	_ = prepare(t, a, CompactionTriggerPressure)
	if got := rec.held(); len(got) != 0 {
		t.Fatalf("a hold with nothing due was announced: %+v", got)
	}
}
