//go:build darwin

package computer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

const targetBundle = "io.reasonix.test.computer-target"

// launchTarget builds the test application into a bundle and opens it, so it
// runs with an identity like any other application.
func launchTarget(t *testing.T) string {
	t.Helper()
	dir := testenv.TempDir(t)
	macos := filepath.Join(dir, "Target.app", "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>` + targetBundle + `</string>
<key>CFBundleExecutable</key><string>target</string>
<key>CFBundleName</key><string>Computer Target</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(dir, "Target.app", "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("swiftc", "-O", "testdata/target.swift", "-o", filepath.Join(macos, "target"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build target: %v\n%s", err, out)
	}
	log := filepath.Join(dir, "target.log")
	if out, err := exec.Command("open", "-g", "-n", filepath.Join(dir, "Target.app"), "--args", log).CombinedOutput(); err != nil {
		t.Fatalf("open target: %v\n%s", err, out)
	}
	// Two instances of one bundle are two answers to "which application is
	// this", and the one that is on its way out captures nothing: the next test
	// waits out the screenshot timeout instead of failing for a reason.
	t.Cleanup(func() {
		_ = exec.Command("pkill", "-f", filepath.Join(macos, "target")).Run()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if exec.Command("pgrep", "-f", filepath.Join(macos, "target")).Run() != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Error("the target application was still running when the test ended")
	})
	waitLog(t, log, "ready")
	return log
}

func TestLiveOperatesAnApplicationWithoutItsPointer(t *testing.T) {
	s := liveSession(t)
	log := launchTarget(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	snap, err := s.Snapshot(ctx, targetBundle)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	t.Logf("snapshot:\n%s", strings.Join(snap.Lines, "\n"))
	field := lineRef(t, snap.Lines, `textField "Probe field"`)
	button := lineRef(t, snap.Lines, `button "Probe button"`)

	res, err := s.Act(ctx, targetBundle, []Step{
		{Action: "set_value", Ref: field, Text: "from-ax"},
		{Action: "click", Ref: button},
		{Action: "focus", Ref: field},
		{Action: "type", Text: " 李雷"},
	})
	if err != nil {
		t.Fatalf("Act: %v (done %d)", err, res.Done)
	}
	waitLog(t, log, "button pressed")
	waitLog(t, log, "from-ax 李雷")

	shot, app, err := s.Screenshot(ctx, targetBundle)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if !strings.HasPrefix(shot, "data:image/") || app.Bundle != targetBundle {
		t.Fatalf("screenshot = %.40q for %+v", shot, app)
	}
	// The button's centre in the window, in points, scaled into the image.
	s.mu.Lock()
	geometry := s.shots[targetBundle]
	s.mu.Unlock()
	x, y := 345/geometry.scale, (geometry.bounds.Height-162)/geometry.scale
	if _, err := s.Act(ctx, targetBundle, []Step{{Action: "click", X: &x, Y: &y}}); err != nil {
		t.Fatalf("click at a screenshot point: %v", err)
	}
	cx, cy := 100/geometry.scale, (geometry.bounds.Height-70)/geometry.scale
	if _, err := s.Act(ctx, targetBundle, []Step{{Action: "click", X: &cx, Y: &cy}}); CodeOf(err) != CodeNoAction {
		t.Fatalf("clicking a view with no accessibility action = %v, want %s", err, CodeNoAction)
	}
	if _, err := s.Snapshot(ctx, "com.apple.Terminal"); CodeOf(err) != CodeAppRefused {
		t.Fatalf("a terminal = %v, want %s", err, CodeAppRefused)
	}
}

// The three a person does without thinking and an agent could not: open an
// element's own context menu, bring something into view, and wait.
func TestLiveContextMenuScrollAndWait(t *testing.T) {
	s := liveSession(t)
	log := launchTarget(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	snap, err := s.Snapshot(ctx, targetBundle)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	t.Logf("snapshot:\n%s", strings.Join(snap.Lines, "\n"))
	menu := lineRef(t, snap.Lines, `"Probe menu area"`)
	res, err := s.Act(ctx, targetBundle, []Step{
		{Action: "right_click", Ref: menu},
		{Action: "wait", Ms: 400},
	})
	if err != nil {
		t.Fatalf("right_click: %v (%v)", err, res.Notes)
	}
	waitLog(t, log, "menu opened")

	// Where the scroll area sits is what says the element was brought into
	// view: the application that offers pages rather than a reveal never hears
	// about the element at all.
	position := func() string {
		now, err := s.Snapshot(ctx, targetBundle)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		for _, line := range now.Lines {
			if strings.Contains(line, "scrollBar") {
				return line
			}
		}
		return ""
	}
	before := position()
	deep := lineRef(t, snap.Lines, `"Deep row"`)
	res, err = s.Act(ctx, targetBundle, []Step{{Action: "scroll", Ref: deep}})
	if err != nil || len(res.Notes) == 0 || !strings.Contains(res.Notes[0], "into view") {
		t.Fatalf("scroll to a ref: %v %v", err, res.Notes)
	}
	if after := position(); after == before {
		t.Fatalf("the scroll area did not move: %q", after)
	}

	if _, err := s.Act(ctx, targetBundle, []Step{{Action: "scroll", Amount: -3}}); err != nil {
		t.Fatalf("scroll by lines: %v", err)
	}
	if _, err := s.Act(ctx, targetBundle, []Step{{Action: "right_click"}}); CodeOf(err) != CodeBadStep {
		t.Fatalf("a right_click with no ref = %v, want %s", err, CodeBadStep)
	}
}

// Pasting is the clipboard borrowed: the application gets the text at once and
// the person gets their clipboard back.
func TestLivePasteLeavesTheClipboardAsItFoundIt(t *testing.T) {
	s := liveSession(t)
	log := launchTarget(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	held := "什么 the person had copied"
	board := exec.Command("pbcopy")
	board.Stdin = strings.NewReader(held)
	if err := board.Run(); err != nil {
		t.Fatalf("pbcopy: %v", err)
	}

	snap, err := s.Snapshot(ctx, targetBundle)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	field := lineRef(t, snap.Lines, `textField "Probe field"`)
	pasted := "粘贴 without typing"
	paste := []Step{{Action: "focus", Ref: field}, {Action: "paste", Text: pasted}}
	// Behind, the application never handles the keystroke, and this says so
	// rather than reporting a paste that put nothing anywhere.
	if _, err := s.Act(ctx, targetBundle, paste); CodeOf(err) != CodeNeedsFront {
		t.Fatalf("pasting into an application that is behind = %v, want %s", err, CodeNeedsFront)
	}

	// Bringing it forward is a pointer step, which is answered for on its own.
	if _, _, err := s.Screenshot(ctx, targetBundle); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	s.mu.Lock()
	geometry := s.shots[targetBundle]
	s.mu.Unlock()
	x, y := 345/geometry.scale, (geometry.bounds.Height-162)/geometry.scale
	if _, err := s.Act(ctx, targetBundle, []Step{{Action: "pointer_click", X: &x, Y: &y}}); err != nil {
		t.Fatalf("bring it forward: %v", err)
	}
	if _, err := s.Act(ctx, targetBundle, paste); err != nil {
		t.Fatalf("paste: %v", err)
	}
	waitLog(t, log, "text "+pasted)

	// It comes back after the application has had its chance at it, not before.
	deadline := time.Now().Add(6 * time.Second)
	for {
		back, err := exec.Command("pbpaste").Output()
		if err != nil {
			t.Fatalf("pbpaste: %v", err)
		}
		if string(back) == held {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the clipboard was left holding %q, not what the person had (%q)", back, held)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// What the accessibility path cannot reach: a view that draws itself and takes
// no action. The pointer is the person's, so it is asked for apart from the
// application and handed back where it was found.
func TestLiveThePointerReachesWhatAccessibilityCannot(t *testing.T) {
	s := liveSession(t)
	log := launchTarget(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	shot, app, err := s.Screenshot(ctx, targetBundle)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if shot == "" || app.Bundle != targetBundle {
		t.Fatalf("screenshot = %.30q for %+v", shot, app)
	}
	s.mu.Lock()
	geometry := s.shots[targetBundle]
	s.mu.Unlock()
	// The drawn view sits in the window's bottom-left corner, in the pixels of
	// the screenshot the model was shown.
	x, y := 100/geometry.scale, (geometry.bounds.Height-70)/geometry.scale

	if _, err := s.Act(ctx, targetBundle, []Step{{Action: "click", X: &x, Y: &y}}); CodeOf(err) != CodeNoAction {
		t.Fatalf("an accessibility click on a drawn view = %v, want %s", err, CodeNoAction)
	}

	// Where the person's pointer is, in the pixels of the screenshot the model
	// was shown, before and after the agent borrows it.
	home, err := s.Act(ctx, targetBundle, []Step{{Action: "pointer_position"}})
	if err != nil {
		t.Fatalf("pointer_position: %v", err)
	}
	res, err := s.Act(ctx, targetBundle, []Step{{Action: "pointer_click", X: &x, Y: &y}})
	if err != nil {
		t.Fatalf("pointer_click: %v (%v)", err, res.Notes)
	}
	waitLog(t, log, "mouseDown")
	back, err := s.Act(ctx, targetBundle, []Step{{Action: "pointer_position"}})
	if err != nil {
		t.Fatalf("pointer_position: %v", err)
	}
	if back.Notes[0] != home.Notes[0] {
		t.Fatalf("the pointer was left at %q, not where the person had it (%q)", back.Notes[0], home.Notes[0])
	}

	toX, toY := x+40, y-20
	if _, err := s.Act(ctx, targetBundle, []Step{{Action: "pointer_drag", X: &x, Y: &y, ToX: &toX, ToY: &toY}}); err != nil {
		t.Fatalf("pointer_drag: %v", err)
	}
	waitLog(t, log, "mouseDragged")
}
