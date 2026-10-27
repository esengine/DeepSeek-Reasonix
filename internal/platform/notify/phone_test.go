package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

type reportedPhone struct {
	kind  string
	count int
}

type phoneRecorder struct {
	mu   sync.Mutex
	got  []reportedPhone
	fail error
	done chan struct{}
}

func newPhoneRecorder() *phoneRecorder { return &phoneRecorder{done: make(chan struct{}, 16)} }

func (r *phoneRecorder) NotifyPhones(_ context.Context, kind string, count int) error {
	r.mu.Lock()
	r.got = append(r.got, reportedPhone{kind, count})
	r.mu.Unlock()
	r.done <- struct{}{}
	return r.fail
}

func (r *phoneRecorder) wait(t *testing.T, n int) []reportedPhone {
	t.Helper()
	for range n {
		select {
		case <-r.done:
		case <-time.After(2 * time.Second):
			t.Fatalf("expected %d reports, saw %v", n, r.snapshot())
		}
	}
	return r.snapshot()
}

func (r *phoneRecorder) snapshot() []reportedPhone {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]reportedPhone(nil), r.got...)
}

// a manual clock for the folding window.
type windows struct {
	mu      sync.Mutex
	pending []func()
}

func (w *windows) after(_ time.Duration, f func()) func() bool {
	w.mu.Lock()
	w.pending = append(w.pending, f)
	w.mu.Unlock()
	return func() bool { return true }
}

func (w *windows) fire() {
	w.mu.Lock()
	run := w.pending
	w.pending = nil
	w.mu.Unlock()
	for _, f := range run {
		f()
	}
}

func phoneCfg(p config.PhoneNotificationsConfig) config.NotificationsConfig {
	return config.NotificationsConfig{Phone: p}
}

var allOn = config.PhoneNotificationsConfig{Enabled: true, Approval: true, Input: true, Error: true}

func TestThreeOccasionsAreReportedByTheirKindAndNothingElse(t *testing.T) {
	rec := newPhoneRecorder()
	p := NewPhone(rec)
	w := &windows{}
	p.after = w.after
	for kind, e := range map[string]event.Event{
		PhoneApproval: {Kind: event.ApprovalRequest, Approval: event.Approval{ID: "1", Tool: "bash", Subject: "rm -rf /"}},
		PhoneInput:    {Kind: event.AskRequest},
		PhoneError:    {Kind: event.TurnDone, Err: errors.New("provider said: secret detail")},
	} {
		p.Event(phoneCfg(allOn), e)
		got := rec.wait(t, 1)
		if last := got[len(got)-1]; last.kind != kind || last.count != 1 {
			t.Fatalf("%v reported %+v, want %s once", e.Kind, last, kind)
		}
	}
	// None of these is an occasion.
	for _, e := range []event.Event{{Kind: event.TurnDone}, {Kind: event.Text, Text: "hello"}, {Kind: event.ToolResult}, {Kind: event.Notice, Text: "x"}, {Kind: event.TurnStarted}} {
		p.Event(phoneCfg(allOn), e)
	}
	time.Sleep(50 * time.Millisecond)
	if got := rec.snapshot(); len(got) != 3 {
		t.Fatalf("reports = %v, want only the three occasions (a cancelled turn and ordinary events are not)", got)
	}
}

func TestNothingIsReportedUnlessTheSwitchesSayWhich(t *testing.T) {
	rec := newPhoneRecorder()
	p := NewPhone(rec)
	p.after = (&windows{}).after
	events := []event.Event{{Kind: event.ApprovalRequest}, {Kind: event.AskRequest}, {Kind: event.TurnDone, Err: errors.New("x")}}
	for name, cfg := range map[string]config.PhoneNotificationsConfig{
		"master off":      {Approval: true, Input: true, Error: true},
		"all off":         {Enabled: true},
		"zero":            {},
		"only approval":   {Enabled: true, Approval: false, Input: false, Error: false},
		"master off only": {Enabled: false, Approval: true},
	} {
		for _, e := range events {
			p.Event(phoneCfg(cfg), e)
		}
		if got := rec.snapshot(); len(got) != 0 {
			t.Fatalf("%s: reported %v", name, got)
		}
	}
	p.Event(phoneCfg(config.PhoneNotificationsConfig{Enabled: true, Input: true}), events[0])
	p.Event(phoneCfg(config.PhoneNotificationsConfig{Enabled: true, Input: true}), events[2])
	p.Event(phoneCfg(config.PhoneNotificationsConfig{Enabled: true, Input: true}), events[1])
	if got := rec.wait(t, 1); len(got) != 1 || got[0].kind != PhoneInput {
		t.Fatalf("with only input on, reported %v", got)
	}
}

func TestABurstOfOneKindIsOneReportThenOneMoreWithTheCount(t *testing.T) {
	rec := newPhoneRecorder()
	p := NewPhone(rec)
	w := &windows{}
	p.after = w.after
	approval := event.Event{Kind: event.ApprovalRequest}
	p.Event(phoneCfg(allOn), approval)
	rec.wait(t, 1)
	for range 4 {
		p.Event(phoneCfg(allOn), approval)
	}
	if got := rec.snapshot(); len(got) != 1 {
		t.Fatalf("a second report left inside the window: %v", got)
	}
	// Another kind is not held back by the first.
	p.Event(phoneCfg(allOn), event.Event{Kind: event.AskRequest})
	rec.wait(t, 1)
	w.fire()
	got := rec.wait(t, 1)
	if last := got[len(got)-1]; last.kind != PhoneApproval || last.count != 4 {
		t.Fatalf("the folded reports went out as %+v, want approval with count 4", last)
	}
	// With nothing folded in the next window, the kind is idle again.
	w.fire()
	p.Event(phoneCfg(allOn), approval)
	if got := rec.wait(t, 1); got[len(got)-1].count != 1 {
		t.Fatalf("after an idle window a report is a single one: %v", got)
	}
}

func TestTheCountIsCappedAndAFailureDoesNotStopLaterReports(t *testing.T) {
	rec := newPhoneRecorder()
	rec.fail = errors.New("account service down")
	p := NewPhone(rec)
	w := &windows{}
	p.after = w.after
	approval := event.Event{Kind: event.ApprovalRequest}
	p.Event(phoneCfg(allOn), approval)
	rec.wait(t, 1)
	for range 250 {
		p.Event(phoneCfg(allOn), approval)
	}
	w.fire()
	got := rec.wait(t, 1)
	if got[len(got)-1].count != 99 {
		t.Fatalf("count = %d, want it capped at 99", got[len(got)-1].count)
	}
}

func TestASinkReportsToPhonesAfterTheWindowEventAndReadsTheSwitchesLive(t *testing.T) {
	rec := newPhoneRecorder()
	phone := NewPhone(rec)
	phone.after = (&windows{}).after
	set := NewSettings(config.NotificationsConfig{})
	sink := NewSink(nil, nil, i18n.English, set).WithPhone(phone)
	sink.Emit(event.Event{Kind: event.AskRequest})
	time.Sleep(50 * time.Millisecond)
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("reported with every switch off: %v", got)
	}
	set.Store(phoneCfg(config.PhoneNotificationsConfig{Enabled: true, Input: true}))
	sink.Emit(event.Event{Kind: event.AskRequest})
	if got := rec.wait(t, 1); len(got) != 1 || got[0].kind != PhoneInput {
		t.Fatalf("after the switch was flipped, reported %v", got)
	}
}

type senderFunc func(ctx context.Context, kind string, count int) error

func (f senderFunc) NotifyPhones(ctx context.Context, kind string, count int) error {
	return f(ctx, kind, count)
}

func TestAPhoneLinkAttachedLaterIsUsedAndOneNotAttachedIsSilent(t *testing.T) {
	link := &PhoneLink{}
	if err := link.NotifyPhones(context.Background(), PhoneApproval, 1); !errors.Is(err, ErrNoPhoneLink) {
		t.Fatalf("before attaching: %v", err)
	}
	tried := make(chan struct{})
	p := NewPhone(senderFunc(func(ctx context.Context, kind string, count int) error {
		defer close(tried)
		return link.NotifyPhones(ctx, kind, count)
	}))
	p.after = (&windows{}).after
	p.Event(phoneCfg(allOn), event.Event{Kind: event.ApprovalRequest})
	<-tried
	rec := newPhoneRecorder()
	link.Set(rec)
	if err := link.NotifyPhones(context.Background(), PhoneError, 2); err != nil {
		t.Fatal(err)
	}
	if got := rec.snapshot(); len(got) != 1 || got[0] != (reportedPhone{PhoneError, 2}) {
		t.Fatalf("link delivered %v", got)
	}
	var nilLink *PhoneLink
	nilLink.Set(rec)
	if err := nilLink.NotifyPhones(context.Background(), PhoneError, 1); !errors.Is(err, ErrNoPhoneLink) {
		t.Fatalf("nil link: %v", err)
	}
}
