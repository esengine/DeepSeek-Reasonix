//go:build windows

package computer

import (
	"bufio"
	"context"
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

func launchWindowsTarget(t *testing.T) probe {
	t.Helper()
	dir := testenv.TempDir(t)
	exe := filepath.Join(dir, windowsTarget)
	if out, err := exec.Command("go", "build", "-o", exe, "./testdata/wintarget").CombinedOutput(); err != nil {
		t.Fatalf("build target: %v\n%s", err, out)
	}
	p := probe{log: filepath.Join(dir, "target.log")}
	cmd := exec.Command(exe, p.log)
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

	res, err := s.Act(ctx, windowsTarget, []Step{
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
	waitLog(t, p.log, "button pressed 1")
	waitLog(t, p.log, "text from-ax 李雷3\ntext \n")

	shot, app, err := s.Screenshot(ctx, windowsTarget)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.HasPrefix(shot, "data:image/") || app.Bundle != windowsTarget {
		t.Fatalf("screenshot = %.40q for %+v", shot, app)
	}
	x, y := inShot(s, p.button)
	if _, err := s.Act(ctx, windowsTarget, []Step{{Action: "click", X: x, Y: y}}); err != nil {
		t.Fatalf("click at a screenshot point: %v", err)
	}
	waitLog(t, p.log, "button pressed 2")
	cx, cy := inShot(s, p.canvas)
	if _, err := s.Act(ctx, windowsTarget, []Step{{Action: "click", X: cx, Y: cy}}); CodeOf(err) != CodeNoAction {
		t.Fatalf("clicking a window with no accessibility action = %v, want %s", err, CodeNoAction)
	}
	if _, err := s.Act(ctx, windowsTarget, []Step{{Action: "key", Key: "meta+s"}}); CodeOf(err) != CodeBadStep {
		t.Fatalf("the Windows key = %v, want %s", err, CodeBadStep)
	}
	if _, err := s.Act(ctx, windowsTarget, []Step{{Action: "scroll", Amount: -3}}); err != nil {
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
	if _, err := s.Act(ctx, windowsTarget, []Step{{Action: "focus", Ref: field}, {Action: "paste", Text: pasted}}); err != nil {
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

	if _, _, err := s.Screenshot(ctx, windowsTarget); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	x, y := inShot(s, p.canvas)
	home, err := s.Act(ctx, windowsTarget, []Step{{Action: "pointer_position"}})
	if err != nil {
		t.Fatalf("pointer_position: %v", err)
	}
	if res, err := s.Act(ctx, windowsTarget, []Step{{Action: "pointer_click", X: x, Y: y}}); err != nil {
		t.Fatalf("pointer_click: %v (%v)", err, res.Notes)
	}
	waitLog(t, p.log, "mouseDown")
	back, err := s.Act(ctx, windowsTarget, []Step{{Action: "pointer_position"}})
	if err != nil {
		t.Fatalf("pointer_position: %v", err)
	}
	if back.Notes[0] != home.Notes[0] {
		t.Fatalf("the pointer was left at %q, not where the person had it (%q)", back.Notes[0], home.Notes[0])
	}
	toX, toY := *x+40, *y-20
	if _, err := s.Act(ctx, windowsTarget, []Step{{Action: "pointer_drag", X: x, Y: y, ToX: &toX, ToY: &toY}}); err != nil {
		t.Fatalf("pointer_drag: %v", err)
	}
	waitLog(t, p.log, "mouseDragged")
}
