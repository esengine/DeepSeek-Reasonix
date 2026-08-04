package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseInterval(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"30s", "*/1 * * * *", true},
		{"5m", "*/5 * * * *", true},
		{"10m", "*/10 * * * *", true},
		{"15m", "*/15 * * * *", true},
		{"30m", "*/30 * * * *", true},
		{"60m", "*/60 * * * *", true},
		{"7m", "*/6 * * * *", true},   // rounds to nearest divisor of 60
		{"45m", "*/30 * * * *", true}, // tie -> smaller step
		{"2h", "0 */2 * * *", true},
		{"6h", "0 */6 * * *", true},
		{"7h", "0 */6 * * *", true}, // rounds to nearest divisor of 24
		{"1d", "0 9 * * *", true},
		{"2d", "0 9 */2 * *", true},
		{"", "", false},
		{"5", "", false},
		{"5x", "", false},
		{"0m", "", false},
		{"-1m", "", false},
		{"abc", "", false},
	}
	for _, c := range cases {
		got, ok := ParseInterval(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseInterval(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNextCron(t *testing.T) {
	base := time.Date(2026, 3, 2, 10, 15, 0, 0, time.Local) // Monday
	cases := []struct {
		cron  string
		after time.Time
		want  string
	}{
		{"*/5 * * * *", base, "2026-03-02 10:20:00"},
		{"*/5 * * * *", time.Date(2026, 3, 2, 10, 20, 0, 0, time.Local), "2026-03-02 10:25:00"},
		{"0 * * * *", base, "2026-03-02 11:00:00"},
		{"30 14 * * *", base, "2026-03-02 14:30:00"},
		{"0 9 * * 1-5", base, "2026-03-03 09:00:00"},     // next weekday (Tue)
		{"0 9 * * 0", base, "2026-03-08 09:00:00"},       // next Sunday
		{"0 0 1 * *", base, "2026-04-01 00:00:00"},       // next 1st of month
		{"0 0 29 2 *", base, "2028-02-29 00:00:00"},      // next leap day
		{"1,15,30 9 * * *", base, "2026-03-03 09:01:00"}, // next fire in minutes 1,15,30 of hour 9
		{"bad cron", base, "0001-01-01 00:00:00"},        // zero time
	}
	for _, c := range cases {
		got := Next(c.cron, c.after)
		var want time.Time
		if c.want != "0001-01-01 00:00:00" {
			// parse expected local time string
			tm, err := time.ParseInLocation("2006-01-02 15:04:05", c.want, time.Local)
			if err != nil {
				t.Fatalf("bad want time %q: %v", c.want, err)
			}
			want = tm
		}
		if !got.Equal(want) {
			t.Errorf("Next(%q, %v) = %v, want %v", c.cron, c.after, got, want)
		}
	}
}

func TestNextStrictlyAfter(t *testing.T) {
	cron := "*/5 * * * *"
	at := time.Date(2026, 3, 2, 10, 15, 0, 0, time.Local)
	got := Next(cron, at)
	if !got.After(at) {
		t.Errorf("Next must be strictly after; got %v", got)
	}
}

func TestAddFireDelete(t *testing.T) {
	s := New()
	defer s.Stop()
	s.Start()

	var fired []Task
	s.OnFire(func(t Task) { fired = append(fired, t) })

	id, err := s.Add("*/1 * * * *", "check deploy", time.Time{}, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(id) != 8 {
		t.Errorf("id %q: want 8 chars", id)
	}
	if s.Count() != 1 {
		t.Errorf("Count = %d, want 1", s.Count())
	}
	if !s.Delete(id) {
		t.Error("Delete returned false for existing task")
	}
	if s.Count() != 0 {
		t.Errorf("Count after delete = %d, want 0", s.Count())
	}
}

func TestTaskLimit(t *testing.T) {
	s := New()
	defer s.Stop()
	for i := 0; i < DefaultTaskLimit; i++ {
		if _, err := s.Add("*/5 * * * *", "x", time.Time{}, false); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}
	if _, err := s.Add("*/5 * * * *", "x", time.Time{}, false); err != ErrTaskLimit {
		t.Errorf("Add over limit: got %v, want ErrTaskLimit", err)
	}
}

func TestDynamicWakeup(t *testing.T) {
	s := New()
	defer s.Stop()
	// dynamic task: no cron, immediate first fire
	if _, err := s.Add("", "watch pr", time.Now(), false); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !s.HasDynamic() {
		t.Error("HasDynamic = false after adding dynamic task")
	}
	if n := s.ScheduleWakeup(5 * time.Minute); n != 1 {
		t.Errorf("ScheduleWakeup = %d, want 1", n)
	}
	if !s.HasPendingDynamic() {
		t.Error("HasPendingDynamic = false after wakeup set")
	}
	if n := s.StopWakeup(); n != 1 {
		t.Errorf("StopWakeup = %d, want 1", n)
	}
	if s.HasPendingDynamic() {
		t.Error("HasPendingDynamic = true after stop")
	}
	if !s.HasDynamic() {
		t.Error("HasDynamic = false after pause (task still exists)")
	}
}

func TestFireDueDynamicConsumesWakeup(t *testing.T) {
	s := New()
	var fired []Task
	s.OnFire(func(t Task) { fired = append(fired, t) })
	id, err := s.Add("", "watch", time.Now(), false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.fireDue()
	if len(fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(fired))
	}
	s.mu.Lock()
	task := s.tasks[id]
	hasPending := !task.NextFire.IsZero()
	s.mu.Unlock()
	if hasPending {
		t.Error("dynamic task still has pending wakeup after fire")
	}
	// second fireDue must not fire again (wakeup consumed)
	s.fireDue()
	if len(fired) != 1 {
		t.Errorf("fired = %d after second fireDue, want 1", len(fired))
	}
}

func TestFireDueCoalescesWhileFiring(t *testing.T) {
	s := New()
	var fired []Task
	s.OnFire(func(t Task) { fired = append(fired, t) })
	id, err := s.Add("*/1 * * * *", "check", time.Time{}, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// force the task due now (Add rolls NextFire to the next minute boundary)
	s.mu.Lock()
	s.tasks[id].NextFire = time.Now().Add(-time.Second)
	s.mu.Unlock()

	s.fireDue()
	if len(fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(fired))
	}
	// simulate a parked turn: the scheduled body has not started yet
	s.fireDue()
	if len(fired) != 1 {
		t.Fatalf("fired = %d after second fireDue while firing, want 1 (coalesced)", len(fired))
	}
	// the turn starts: clear the flag, then a later due cycle may fire again
	s.MarkStarted(id)
	s.mu.Lock()
	task := s.tasks[id]
	next := task.NextFire
	s.mu.Unlock()
	if task.firing {
		t.Error("MarkStarted did not clear the firing flag")
	}
	if next.IsZero() || !next.After(time.Now()) {
		t.Errorf("NextFire should have rolled forward for a cron task, got %v", next)
	}
}

func TestNextDue(t *testing.T) {
	s := New()
	defer s.Stop()
	if _, _, ok := s.NextDue(); ok {
		t.Error("NextDue = ok with no tasks")
	}
	laterID, err := s.Add("", "later", time.Now().Add(10*time.Minute), false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	soonerID, err := s.Add("", "sooner", time.Now().Add(2*time.Minute), false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	id, at, ok := s.NextDue()
	if !ok || id != soonerID {
		t.Errorf("NextDue = (%q, %v, %v), want sooner task %q", id, at, ok, soonerID)
	}
	if until := time.Until(at); until < time.Minute || until > 3*time.Minute {
		t.Errorf("NextDue time %v not within expected window", at)
	}
	// a paused (zero NextFire) task must be ignored
	s.mu.Lock()
	if t, ok := s.tasks[laterID]; ok {
		t.NextFire = time.Time{}
	}
	s.mu.Unlock()
	id, _, ok = s.NextDue()
	if !ok || id != soonerID {
		t.Errorf("NextDue after pause = (%q, %v), want %q", id, ok, soonerID)
	}
}

func TestCronTurnLongerThanIntervalNoStampede(t *testing.T) {
	s := New()
	var fired []Task
	s.OnFire(func(t Task) { fired = append(fired, t) })
	id, err := s.Add("*/1 * * * *", "check", time.Time{}, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// deliver the :00 slot
	s.mu.Lock()
	s.tasks[id].NextFire = time.Now().Add(-time.Second)
	s.mu.Unlock()

	s.fireDue()
	if len(fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(fired))
	}
	// the turn is parked/starting for longer than the interval: ticks pass,
	// the slot stays in the past, but firing blocks re-delivery
	for i := 0; i < 3; i++ {
		s.fireDue()
	}
	if len(fired) != 1 {
		t.Fatalf("fired = %d after 3 ticks while firing, want 1 (no stampede)", len(fired))
	}
	// the turn starts: re-arm skips the missed cycles, arming a future slot
	s.MarkStarted(id)
	s.mu.Lock()
	next := s.tasks[id].NextFire
	s.mu.Unlock()
	if next.IsZero() || !next.After(time.Now()) {
		t.Fatalf("MarkStarted did not re-arm a future fire, got %v", next)
	}
	s.fireDue()
	if len(fired) != 1 {
		t.Fatalf("fired = %d after re-arm, want 1 (future slot not due)", len(fired))
	}
	// once the re-armed slot passes, the next cycle fires normally
	s.mu.Lock()
	s.tasks[id].NextFire = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.fireDue()
	if len(fired) != 2 {
		t.Fatalf("fired = %d after next due cycle, want 2", len(fired))
	}
}

func TestReleaseFiringAllowsRefire(t *testing.T) {
	s := New()
	var fired []Task
	s.OnFire(func(t Task) { fired = append(fired, t) })
	id, err := s.Add("*/1 * * * *", "check", time.Time{}, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.mu.Lock()
	s.tasks[id].NextFire = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.fireDue()
	if len(fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(fired))
	}
	// admission dropped the turn; release the flag like runScheduledTurn does
	s.ReleaseFiring(id)
	s.fireDue()
	if len(fired) != 2 {
		t.Fatalf("fired = %d after ReleaseFiring + tick, want 2 (cron re-fires)", len(fired))
	}
}

func TestCancelDynamicKeepsCronTasks(t *testing.T) {
	s := New()
	if _, err := s.Add("*/5 * * * *", "cron loop", time.Time{}, false); err != nil {
		t.Fatalf("Add cron: %v", err)
	}
	if _, err := s.Add("", "dynamic loop", time.Now(), false); err != nil {
		t.Fatalf("Add dynamic: %v", err)
	}
	if _, err := s.Add("", "one-shot reminder", time.Now(), true); err != nil {
		t.Fatalf("Add one-shot: %v", err)
	}
	if n := s.CancelDynamic(); n != 2 {
		t.Fatalf("CancelDynamic = %d, want 2 (dynamic + one-shot, both cron-less)", n)
	}
	views := s.Tasks()
	if len(views) != 1 || views[0].CronExpr == "" {
		t.Fatalf("remaining tasks = %+v, want only the cron loop", views)
	}
	if views[0].Prompt != "cron loop" {
		t.Errorf("remaining task = %q, want the fixed-interval loop", views[0].Prompt)
	}
}

func TestStopFlushes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	s := New()
	s.SetPersistPath(path)
	s.Start()
	defer s.Stop()
	if _, err := s.Add("*/5 * * * *", "flush me", time.Time{}, false); err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.Stop() // must flush the just-added task even inside the rate-limit window
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("sidecar not written by Stop: %v", err)
	}
	if !strings.Contains(string(data), "flush me") {
		t.Errorf("sidecar missing the task added before Stop: %s", data)
	}
	// a stopped scheduler may Start again without panicking
	s.Start()
	if _, err := s.Add("*/5 * * * *", "again", time.Time{}, false); err != nil {
		t.Fatalf("Add after restart: %v", err)
	}
	s.Stop()
}

func TestOneShotSelfDeletes(t *testing.T) {
	s := New()
	var fired []Task
	s.OnFire(func(t Task) { fired = append(fired, t) })
	id, err := s.Add("", "remind me", time.Now(), true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.fireDue()
	if len(fired) != 1 {
		t.Fatalf("fired = %d, want 1", len(fired))
	}
	if s.Count() != 0 {
		t.Errorf("Count = %d, want 0 after one-shot fire", s.Count())
	}
	if s.Delete(id) {
		t.Error("one-shot task still deletable after self-delete")
	}
}

func TestPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")

	s := New()
	s.SetPersistPath(path)
	id, err := s.Add("*/5 * * * *", "check ci", time.Time{}, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.Flush() // force save (rate limiter)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	// reload into a fresh scheduler
	s2 := New()
	s2.Load(path)
	if s2.Count() != 1 {
		t.Fatalf("loaded Count = %d, want 1", s2.Count())
	}
	views := s2.Tasks()
	if views[0].ID != id || views[0].Prompt != "check ci" {
		t.Errorf("loaded task = %+v, want id %s prompt check ci", views[0], id)
	}
	if views[0].CronExpr != "*/5 * * * *" {
		t.Errorf("loaded cron = %q", views[0].CronExpr)
	}
	// recurring task rolls forward to a future fire
	nf, _ := time.Parse(time.RFC3339, views[0].NextFire)
	if !nf.After(time.Now()) {
		t.Errorf("loaded NextFire %v not in the future", nf)
	}
}

func TestLoadPrunesExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	old := time.Now().Add(-8 * 24 * time.Hour)
	data, _ := json.Marshal([]Task{
		{ID: "aaaa1111", CronExpr: "*/5 * * * *", Prompt: "old", Created: old, NextFire: time.Now()},
		{ID: "bbbb2222", CronExpr: "*/5 * * * *", Prompt: "fresh", Created: time.Now(), NextFire: time.Now()},
	})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Load(path)
	if s.Count() != 1 {
		t.Fatalf("Count = %d, want 1 (expired pruned)", s.Count())
	}
	if s.Tasks()[0].ID != "bbbb2222" {
		t.Errorf("kept %s, want bbbb2222", s.Tasks()[0].ID)
	}
}

func TestLoadPrunesMissedOneShot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	past := time.Now().Add(-time.Hour)
	data, _ := json.Marshal([]Task{
		{ID: "cccc3333", Prompt: "missed reminder", OneShot: true, Created: time.Now(), NextFire: past},
	})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s := New()
	s.Load(path)
	if s.Count() != 0 {
		t.Errorf("Count = %d, want 0 (missed one-shot pruned)", s.Count())
	}
}

func TestStartIdempotentAndStop(t *testing.T) {
	s := New()
	s.Start()
	s.Start() // no panic
	s.Stop()
	s.Stop() // no panic
}

func TestValid(t *testing.T) {
	for _, good := range []string{"*/5 * * * *", "0 9 * * 1-5", "1,15,30 9 * * *", "0 0 1 1 *"} {
		if !Valid(good) {
			t.Errorf("Valid(%q) = false", good)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "a b c d e", "* * * 13 *"} {
		if Valid(bad) {
			t.Errorf("Valid(%q) = true", bad)
		}
	}
}
