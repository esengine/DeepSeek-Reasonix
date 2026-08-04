package control

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/scheduler"
	"reasonix/internal/tool"
)

func TestParseLoopArgs(t *testing.T) {
	cases := []struct {
		in           string
		wantInterval string
		wantPrompt   string
		wantNoExpire bool
	}{
		{"5m check the deploy", "5m", "check the deploy", false},
		{"30s poll ci", "30s", "poll ci", false},
		{"2h tend the pr", "2h", "tend the pr", false},
		{"check the deploy", "", "check the deploy", false},
		{"", "", "", false},
		{"   ", "", "", false},
		{"5m", "5m", "", false},
		{"5x check", "", "5x check", false}, // not a parseable interval -> whole thing is the prompt
		{"--forever 5m check deploy", "5m", "check deploy", true},
		{"--forever check deploy", "", "check deploy", true},
		{"--forever 5m", "5m", "", true},
		{"--forever", "", "", true},
		{"--forever   ", "", "", true},
	}
	for _, c := range cases {
		interval, prompt, noExpire := parseLoopArgs(c.in)
		if interval != c.wantInterval || prompt != c.wantPrompt || noExpire != c.wantNoExpire {
			t.Errorf("parseLoopArgs(%q) = (%q, %q, %v), want (%q, %q, %v)", c.in, interval, prompt, noExpire, c.wantInterval, c.wantPrompt, c.wantNoExpire)
		}
	}
}

func TestLoopListAndDeleteText(t *testing.T) {
	ctrl := &Controller{scheduler: scheduler.New()}
	defer ctrl.scheduler.Stop()
	if got := ctrl.LoopListText(); got != "no scheduled tasks" {
		t.Errorf("empty list = %q, want no scheduled tasks", got)
	}
	if got := ctrl.LoopDeleteText(""); got != "usage: /loopdel <task-id>" {
		t.Errorf("empty id = %q, want usage", got)
	}
	if got := ctrl.LoopDeleteText("deadbeef"); got != "no scheduled task deadbeef" {
		t.Errorf("unknown id = %q", got)
	}
	if _, err := ctrl.StartLoop("5m check deploy"); err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	if _, err := ctrl.StartLoop("watch pr"); err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	list := ctrl.LoopListText()
	if !strings.Contains(list, "2 scheduled task(s)") {
		t.Errorf("list missing count: %q", list)
	}
	for _, v := range ctrl.scheduler.Tasks() {
		if !strings.Contains(list, v.ID) {
			t.Errorf("list missing task %s: %q", v.ID, list)
		}
		if v.CronExpr == "" && !strings.Contains(list, "dynamic") {
			t.Errorf("list missing dynamic marker: %q", list)
		}
		if got := ctrl.LoopDeleteText(v.ID); got != "deleted scheduled task "+v.ID {
			t.Errorf("delete = %q", got)
		}
	}
	if got := ctrl.LoopListText(); got != "no scheduled tasks" {
		t.Errorf("after deletes = %q, want no scheduled tasks", got)
	}
}

func TestStartLoopForever(t *testing.T) {
	ctrl := &Controller{scheduler: scheduler.New()}
	defer ctrl.scheduler.Stop()
	text, err := ctrl.StartLoop("--forever 5m check deploy")
	if err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	if !strings.Contains(text, "no expiry") {
		t.Errorf("confirmation missing no-expiry note: %q", text)
	}
	views := ctrl.scheduler.Tasks()
	if len(views) != 1 {
		t.Fatalf("tasks = %d, want 1", len(views))
	}
	if !views[0].NoExpire {
		t.Errorf("task not marked NoExpire: %+v", views[0])
	}
	if views[0].CronExpr != "*/5 * * * *" {
		t.Errorf("cron = %q, want */5 * * * *", views[0].CronExpr)
	}
}

func TestStartLoopFixedInterval(t *testing.T) {
	ctrl := &Controller{scheduler: scheduler.New()}
	defer ctrl.scheduler.Stop()
	text, err := ctrl.StartLoop("5m check deploy")
	if err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	if !strings.Contains(text, "every 5m") {
		t.Errorf("confirmation missing interval: %q", text)
	}
	views := ctrl.scheduler.Tasks()
	if len(views) != 1 {
		t.Fatalf("tasks = %d, want 1", len(views))
	}
	if views[0].CronExpr != "*/5 * * * *" {
		t.Errorf("cron = %q, want */5 * * * *", views[0].CronExpr)
	}
	if views[0].Prompt != "check deploy" {
		t.Errorf("prompt = %q", views[0].Prompt)
	}
}

func TestStartLoopDynamic(t *testing.T) {
	ctrl := &Controller{scheduler: scheduler.New()}
	defer ctrl.scheduler.Stop()
	text, err := ctrl.StartLoop("check deploy")
	if err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	if !strings.Contains(text, "dynamic") {
		t.Errorf("confirmation missing dynamic: %q", text)
	}
	views := ctrl.scheduler.Tasks()
	if len(views) != 1 || views[0].CronExpr != "" {
		t.Fatalf("want one dynamic task, got %+v", views)
	}
}

func TestStartLoopBareUsesMaintenancePrompt(t *testing.T) {
	ctrl := &Controller{scheduler: scheduler.New()}
	defer ctrl.scheduler.Stop()
	if _, err := ctrl.StartLoop(""); err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	views := ctrl.scheduler.Tasks()
	if len(views) != 1 {
		t.Fatalf("tasks = %d, want 1", len(views))
	}
	if !strings.Contains(views[0].Prompt, "Continue the current session's ongoing work") {
		t.Errorf("bare /loop did not use the maintenance prompt: %q", views[0].Prompt)
	}
}

func TestStartLoopUsesLoopMD(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".reasonix")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := "Tend the release/next PR and keep it green."
	if err := os.WriteFile(filepath.Join(dir, "loop.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl := &Controller{scheduler: scheduler.New(), workspaceRoot: root}
	defer ctrl.scheduler.Stop()
	if _, err := ctrl.StartLoop(""); err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	views := ctrl.scheduler.Tasks()
	if len(views) != 1 || views[0].Prompt != custom {
		t.Errorf("loop.md not used: %+v", views)
	}
}

func TestRunScheduledTurnNoticePerStartedTurn(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("done.")}}
	reg := tool.NewRegistry()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

	turnDone := make(chan struct{}, 4)
	var notices []string
	var mu sync.Mutex
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e.Text)
			mu.Unlock()
		}
		if e.Kind == event.TurnDone {
			select {
			case turnDone <- struct{}{}:
			default:
			}
		}
	})
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	defer c.Close()

	// started turn: exactly one "running" notice, with the task ID
	c.runScheduledTurn(scheduler.Task{ID: "abcd1234", Prompt: "check deploy", CronExpr: "*/1 * * * *"})
	select {
	case <-turnDone:
	case <-time.After(10 * time.Second):
		t.Fatal("scheduled turn did not finish")
	}
	mu.Lock()
	got := append([]string(nil), notices...)
	mu.Unlock()
	if len(got) != 1 || !strings.Contains(got[0], "abcd1234") || !strings.Contains(got[0], "running") {
		t.Fatalf("notices = %v, want exactly one 'â° scheduled task abcd1234 running'", got)
	}

	// dropped turn (controller rotating): no notice for it, and the firing
	// flag is released so the scheduler is not wedged
	mu.Lock()
	notices = nil
	mu.Unlock()
	c.mu.Lock()
	c.rotating = true
	c.mu.Unlock()
	c.runScheduledTurn(scheduler.Task{ID: "efgh5678", Prompt: "x", CronExpr: "*/1 * * * *"})
	c.mu.Lock()
	c.rotating = false
	c.mu.Unlock()
	mu.Lock()
	got = append([]string(nil), notices...)
	mu.Unlock()
	for _, n := range got {
		if strings.Contains(n, "efgh5678") {
			t.Fatalf("dropped turn emitted a notice: %v", got)
		}
	}
}

func TestRunScheduledTurnClosedReleasesFiring(t *testing.T) {
	sched := scheduler.New()
	delivered := make(chan string, 8)
	sched.OnFire(func(t scheduler.Task) { delivered <- t.ID })
	sched.Start()
	defer sched.Stop()
	id, err := sched.Add("", "watch", time.Now(), false, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	select {
	case got := <-delivered:
		if got != id {
			t.Fatalf("first delivery = %q, want %q", got, id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first delivery never fired")
	}
	// At this point the task is firing (delivered, body not started). Run the
	// closed-controller path: it must release the flag so a later wakeup can
	// deliver again.
	ctrl := &Controller{scheduler: sched, sink: event.Discard}
	ctrl.mu.Lock()
	ctrl.closed = true
	ctrl.mu.Unlock()
	ctrl.runScheduledTurn(scheduler.Task{ID: id, Prompt: "watch"})

	sched.ScheduleWakeup(time.Millisecond)
	select {
	case got := <-delivered:
		if got != id {
			t.Fatalf("re-delivery = %q, want %q", got, id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("firing flag not released: no re-delivery after wakeup")
	}
}

func TestStartLoopErrors(t *testing.T) {
	ctrl := &Controller{scheduler: nil}
	if _, err := ctrl.StartLoop("5m x"); err == nil {
		t.Error("StartLoop with nil scheduler should error")
	}

	s := scheduler.New()
	defer s.Stop()
	ctrl = &Controller{scheduler: s}
	for i := 0; i < scheduler.DefaultTaskLimit; i++ {
		if _, err := ctrl.StartLoop("check"); err != nil {
			t.Fatalf("StartLoop %d: %v", i, err)
		}
	}
	if _, err := ctrl.StartLoop("check"); err == nil {
		t.Error("StartLoop over the task limit should error")
	}
}
