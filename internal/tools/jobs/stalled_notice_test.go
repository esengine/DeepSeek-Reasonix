package jobs

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

func TestStalledJobEmitsCodedNoticeWithTheQuietSpan(t *testing.T) {
	sink := &recordingSink{}
	m := NewManager(sink)
	defer m.Close()
	m.stalledWarning = 15 * time.Minute
	m.recordStalled("", "bash-16", "bash", "make build", 0)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, ev := range sink.events {
		if ev.Code != event.NoticeCodeJobStalled {
			continue
		}
		p, ok := event.DecodeJobNotice(ev.Detail)
		if !ok || p.Kind != "bash" || p.ID != "bash-16" || p.Label != "make build" || p.QuietSeconds != 900 || ev.Level != event.LevelWarn {
			t.Fatalf("stalled notice = %+v payload %+v", ev, p)
		}
		if !strings.Contains(ev.Text, "has been inactive for 15m0s") || !strings.Contains(ev.Text, "no progress has been reported") || !event.DetailIsPayload(ev.Code) {
			t.Fatalf("English fallback = %q, payload flag %v", ev.Text, event.DetailIsPayload(ev.Code))
		}
		return
	}
	t.Fatalf("no %s notice in %+v", event.NoticeCodeJobStalled, sink.events)
}

// A job that reported steps and then went quiet is a different report from one
// that never reported any: the first was working and is now waiting, the second
// never got going, and the reader's next move differs.
func TestStalledNoticeSaysWhatTheJobHadReported(t *testing.T) {
	sink := &recordingSink{}
	m := NewManager(sink)
	defer m.Close()
	m.stalledWarning = 15 * time.Minute
	m.recordStalled("", "task-7", "task", "audit", 42)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, ev := range sink.events {
		if ev.Code != event.NoticeCodeJobStalled {
			continue
		}
		if !strings.Contains(ev.Text, "42 step(s) were reported") {
			t.Fatalf("notice = %q, want the step count it reported", ev.Text)
		}
		return
	}
	t.Fatalf("no %s notice in %+v", event.NoticeCodeJobStalled, sink.events)
}
