//go:build windows

package computer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

const windowsTarget = "computer-target.exe"

// probe is the running test application and where it said its parts are, in
// screen pixels.
type probe struct {
	log            string
	button, canvas [2]float64
}

// launchWindowsTarget starts the test application; mode is passed on as its
// second argument.
func launchWindowsTarget(t *testing.T, mode ...string) probe {
	t.Helper()
	dir := testenv.TempDir(t)
	exe := filepath.Join(dir, windowsTarget)
	if out, err := exec.Command("go", "build", "-o", exe, "./testdata/wintarget").CombinedOutput(); err != nil {
		t.Fatalf("build target: %v\n%s", err, out)
	}
	p := probe{log: filepath.Join(dir, "target.log")}
	cmd := exec.Command(exe, append([]string{p.log}, mode...)...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start target: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitLog(t, p.log, "ready")
	f, err := os.Open(p.log)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var name string
		var x, y float64
		if n, _ := fmt.Sscanf(sc.Text(), "at %s %f %f", &name, &x, &y); n == 3 {
			switch name {
			case "button":
				p.button = [2]float64{x, y}
			case "canvas":
				p.canvas = [2]float64{x, y}
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return p
}

// inShot converts a point on screen into the pixels of the application's
// latest screenshot, which is the frame the model aims in.
func inShot(s *Session, at [2]float64) (*float64, *float64) {
	s.mu.Lock()
	g := s.shots[windowsTarget]
	s.mu.Unlock()
	x, y := (at[0]-g.bounds.X)/g.scale, (at[1]-g.bounds.Y)/g.scale
	return &x, &y
}

func TestLiveWindowsOperatesAnApplication(t *testing.T) {
	s := liveSession(t)
	p := launchWindowsTarget(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	snap, err := s.Snapshot(ctx, windowsTarget)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	t.Logf("snapshot:\n%s", strings.Join(snap.Lines, "\n"))
	field := lineRef(t, snap.Lines, `edit "Probe field"`)
	button := lineRef(t, snap.Lines, `button "Probe button"`)

	res, err := s.Act(ctx, windowsTarget, 0, []Step{
		{Action: "set_value", Ref: field, Text: "from-ax"},
		{Action: "click", Ref: button},
		{Action: "focus", Ref: field},
		{Action: "key", Key: "end"},
		{Action: "type", Text: " 李雷"},
		{Action: "key", Key: "3"},
		{Action: "key", Key: "shift+home"},
		{Action: "key", Key: "backspace"},
	})
	if err != nil {
		t.Fatalf("Act: %v (done %d)", err, res.Done)
	}
	if e := res.Steps[4].Effect; e.Class != EffectConfirmed {
		t.Errorf("typing after the end of a value = %+v, want confirmed", e)
	}
	waitLog(t, p.log, "button pressed 1")
	waitLog(t, p.log, "text from-ax 李雷3\ntext \n")

	shot, app, err := s.Screenshot(ctx, windowsTarget, 0)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.HasPrefix(shot, "data:image/") || app.Bundle != windowsTarget {
		t.Fatalf("screenshot = %.40q for %+v", shot, app)
	}
	x, y := inShot(s, p.button)
	if _, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "click", X: x, Y: y}}); err != nil {
		t.Fatalf("click at a screenshot point: %v", err)
	}
	waitLog(t, p.log, "button pressed 2")
	cx, cy := inShot(s, p.canvas)
	if _, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "click", X: cx, Y: cy}}); CodeOf(err) != CodeNoAction {
		t.Fatalf("clicking a window with no accessibility action = %v, want %s", err, CodeNoAction)
	}
	if _, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "key", Key: "meta+s"}}); CodeOf(err) != CodeBadStep {
		t.Fatalf("the Windows key = %v, want %s", err, CodeBadStep)
	}
	if _, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "scroll", Amount: -3}}); err != nil {
		t.Fatalf("scroll by lines: %v", err)
	}
	for _, name := range []string{"cmd.exe", "PowerShell.exe"} {
		if _, err := s.Snapshot(ctx, name); CodeOf(err) != CodeAppRefused {
			t.Fatalf("%s = %v, want %s", name, err, CodeAppRefused)
		}
	}
}

func clipboard(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw").Output()
	if err != nil {
		t.Fatalf("Get-Clipboard: %v", err)
	}
	return strings.TrimRight(string(out), "\r\n")
}

// Pasting is the clipboard borrowed: the application gets the text at once and
// the person gets their clipboard back.
func TestLiveWindowsPasteGivesTheClipboardBack(t *testing.T) {
	s := liveSession(t)
	p := launchWindowsTarget(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	held := "what the person had copied"
	if err := exec.Command("powershell", "-NoProfile", "-Command", "Set-Clipboard -Value '"+held+"'").Run(); err != nil {
		t.Fatalf("Set-Clipboard: %v", err)
	}
	snap, err := s.Snapshot(ctx, windowsTarget)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	field := lineRef(t, snap.Lines, `edit "Probe field"`)
	pasted := "粘贴 without typing"
	if _, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "focus", Ref: field}, {Action: "paste", Text: pasted}}); err != nil {
		t.Fatalf("paste: %v", err)
	}
	waitLog(t, p.log, "text "+pasted)

	deadline := time.Now().Add(6 * time.Second)
	for {
		if back := clipboard(t); back == held {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("the clipboard was left holding %q, not what the person had (%q)", back, held)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// What the accessibility path cannot reach: a window that draws itself and
// takes no action. The pointer is the person's, so it goes back where it was.
func TestLiveWindowsThePointerReachesWhatAccessibilityCannot(t *testing.T) {
	s := liveSession(t)
	p := launchWindowsTarget(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, _, err := s.Screenshot(ctx, windowsTarget, 0); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	x, y := inShot(s, p.canvas)
	home, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "pointer_position"}})
	if err != nil {
		t.Fatalf("pointer_position: %v", err)
	}
	if res, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "pointer_click", X: x, Y: y}}); err != nil {
		t.Fatalf("pointer_click: %v (%v)", err, res.Steps)
	}
	waitLog(t, p.log, "mouseDown")
	back, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "pointer_position"}})
	if err != nil {
		t.Fatalf("pointer_position: %v", err)
	}
	if back.Steps[0].Note != home.Steps[0].Note {
		t.Fatalf("the pointer was left at %q, not where the person had it (%q)", back.Steps[0].Note, home.Steps[0].Note)
	}
	toX, toY := *x+40, *y-20
	if _, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "pointer_drag", X: x, Y: y, ToX: &toX, ToY: &toY}}); err != nil {
		t.Fatalf("pointer_drag: %v", err)
	}
	waitLog(t, p.log, "mouseDragged")
}

// heldSnapshot reads the application until a modal is holding its input.
func heldSnapshot(t *testing.T, ctx context.Context, s *Session) Snapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		snap, err := s.Snapshot(ctx, windowsTarget)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if len(snap.Modals) > 0 {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("no modal in the snapshot:\n%s", strings.Join(snap.Lines, "\n"))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// actor runs steps on the test application and answers the last one's effect.
func actor(t *testing.T, ctx context.Context, s *Session) func(...Step) Effect {
	return func(steps ...Step) Effect {
		t.Helper()
		res, err := s.Act(ctx, windowsTarget, 0, steps)
		if err != nil || len(res.Steps) == 0 {
			t.Fatalf("Act = %+v, %v", res, err)
		}
		return res.Steps[len(res.Steps)-1].Effect
	}
}

// A message box holds the application's input: typing would land in it, and
// the window behind it takes nothing, so both fail and name the modal.
func TestLiveWindowsAModalHoldsTheInput(t *testing.T) {
	s := liveSession(t)
	p := launchWindowsTarget(t, "modal")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	act := actor(t, ctx, s)

	snap := heldSnapshot(t, ctx, s)
	t.Logf("snapshot:\n%s", strings.Join(snap.Lines, "\n"))
	if m := snap.Modals[0]; m.Title != "Probe modal" || m.Blocks != "Computer Target" || m.Ref == "" {
		t.Fatalf("modal = %+v", m)
	}
	button := lineRef(t, snap.Lines, `button "Probe button"`)
	res, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "type", Text: "hello"}})
	if CodeOf(err) != CodeBlocked || res.Done != 0 {
		t.Fatalf("typing while a message box holds the input = %+v, %v; want %s", res, err, CodeBlocked)
	}
	if f, _ := errors.AsType[*Failure](err); f.BlockedBy == nil || f.BlockedBy.Title != "Probe modal" {
		t.Fatalf("the failure does not name the modal: %v", err)
	}
	if _, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "click", Ref: button}}); CodeOf(err) != CodeBlocked {
		t.Fatalf("pressing a button behind the modal = %v, want %s", err, CodeBlocked)
	}
	if e := act(Step{Action: "key", Key: "escape"}); e.Class != EffectUnverifiable || e.BlockedBy == nil {
		t.Fatalf("Escape into the modal = %+v, want unverifiable and the modal it went to", e)
	}
	waitLog(t, p.log, "modal closed")
	if body, _ := os.ReadFile(p.log); strings.Contains(string(body), "hello") || strings.Contains(string(body), "button pressed") {
		t.Fatalf("input reached the application behind the modal:\n%s", body)
	}

	snap, err = s.Snapshot(ctx, windowsTarget)
	if err != nil || len(snap.Modals) != 0 {
		t.Fatalf("after the modal closed: %+v, %v", snap.Modals, err)
	}
	field := lineRef(t, snap.Lines, `edit "Probe field"`)
	if e := act(Step{Action: "focus", Ref: field}, Step{Action: "type", Text: "abc"}); e.Class != EffectConfirmed {
		t.Fatalf("typing into the field = %+v, want confirmed", e)
	}
	if e := act(Step{Action: "set_value", Ref: field, Text: "set"}); e.Class != EffectConfirmed {
		t.Fatalf("set_value = %+v, want confirmed", e)
	}
	readonly := lineRef(t, snap.Lines, `edit "Probe readonly"`)
	if e := act(Step{Action: "focus", Ref: readonly}, Step{Action: "type", Text: "xyz"}); e.Class != EffectSuspectedNoop {
		t.Fatalf("typing into a read-only field = %+v, want suspected_noop", e)
	}
}

// A modal with a text field is where typing belongs: it goes in, is read
// back, and says which modal it went to.
func TestLiveWindowsTypingIntoAModalsFieldIsReadBack(t *testing.T) {
	s := liveSession(t)
	p := launchWindowsTarget(t, "form")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	act := actor(t, ctx, s)

	snap := heldSnapshot(t, ctx, s)
	if m := snap.Modals[0]; m.Title != "Probe form" || m.Blocks != "Computer Target" {
		t.Fatalf("modal = %+v", m)
	}
	e := act(Step{Action: "type", Text: "xyz"})
	if e.Class != EffectConfirmed || e.BlockedBy == nil || e.BlockedBy.Title != "Probe form" {
		t.Fatalf("typing into the modal's field = %+v, want confirmed into the modal", e)
	}
	waitLog(t, p.log, "form text xyz")
}

// Two top-level windows of one application leave the target ambiguous: input
// is refused before it is sent, and naming a window aims it there.
func TestLiveWindowsTwoWindowsAreRefusedUntilOneIsNamed(t *testing.T) {
	s := liveSession(t)
	p := launchWindowsTarget(t, "second")
	waitLog(t, p.log, "second open")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	apps, err := s.Apps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var app App
	for _, a := range apps {
		if a.Bundle == windowsTarget {
			app = a
		}
	}
	main, second := 0, 0
	for _, w := range app.Windows {
		switch w.Title {
		case "Computer Target":
			main = w.ID
		case "Second Window":
			second = w.ID
		}
		if w.Owned == nil || *w.Owned {
			t.Fatalf("window %+v is not reported as an independent window", w)
		}
	}
	if main == 0 || second == 0 {
		t.Fatalf("windows = %+v", app.Windows)
	}

	res, err := s.Act(ctx, windowsTarget, 0, []Step{{Action: "type", Text: "hello"}})
	f, _ := errors.AsType[*Failure](err)
	if f == nil || f.Code != CodeAmbiguousWindow || res.Done != 0 || len(f.Candidates) != 2 {
		t.Fatalf("typing at two windows = %+v, %v; want %s with both candidates", res, err, CodeAmbiguousWindow)
	}
	if _, _, err := s.Screenshot(ctx, windowsTarget, 0); CodeOf(err) != CodeAmbiguousWindow {
		t.Fatalf("a screenshot of two windows = %v, want %s", err, CodeAmbiguousWindow)
	}
	if body, _ := os.ReadFile(p.log); strings.Contains(string(body), "hello") {
		t.Fatalf("input reached the application despite the refusal:\n%s", body)
	}

	if _, _, err := s.Screenshot(ctx, windowsTarget, second); err != nil {
		t.Fatalf("screenshot of the second window: %v", err)
	}
	s.mu.Lock()
	shot := s.shots[windowsTarget]
	s.mu.Unlock()
	if shot.window != second || shot.bounds.Width < 250 || shot.bounds.Width > 400 {
		t.Fatalf("the capture is %+v, want the 300-wide second window %d", shot, second)
	}
	snap, err := s.Snapshot(ctx, windowsTarget)
	if err != nil {
		t.Fatal(err)
	}
	field := lineRef(t, snap.Lines, `edit "Probe field"`)
	if _, err := s.Act(ctx, windowsTarget, second, []Step{{Action: "key", Key: "home"}}); err != nil {
		t.Fatalf("raising the second window: %v", err)
	}
	if res, err := s.Act(ctx, windowsTarget, main, []Step{{Action: "focus", Ref: field}, {Action: "type", Text: "hello"}}); err != nil || res.Done != 2 {
		t.Fatalf("typing into the main window = %+v, %v", res, err)
	}
	waitLog(t, p.log, "hello")
	if _, err := s.Act(ctx, windowsTarget, 1, []Step{{Action: "type", Text: "x"}}); CodeOf(err) != CodeNoWindow {
		t.Fatalf("a window the application does not have = %v, want %s", err, CodeNoWindow)
	}
}
