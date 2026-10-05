package computer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/model/visionimage"
)

// fakeHelperEnv makes the test binary answer as the native helper, so the
// protocol, the geometry and the failures run without a real application.
const fakeHelperEnv = "REASONIX_COMPUTER_FAKE_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(fakeHelperEnv) != "" {
		runFakeHelper(os.Stdin, os.Stdout)
		return
	}
	os.Exit(m.Run())
}

func fakeScreenshot() []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4000, 2000)))
	return buf.Bytes()
}

func runFakeHelper(in io.Reader, out io.Writer) {
	enc := json.NewEncoder(out)
	var lastClick map[string]any
	var lastCall map[string]any
	released := 0
	asked := 0
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		var req struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		if req.Method == "_silent" {
			continue
		}
		reply := map[string]any{"id": req.ID, "result": map[string]any{}}
		refuse := func(code, message string) {
			delete(reply, "result")
			reply["error"] = map[string]any{"code": code, "message": message}
		}
		switch req.Method {
		case "apps":
			reply["result"] = map[string]any{"apps": []any{
				map[string]any{"pid": 7, "bundle": "com.example.Notes", "name": "Notes", "windows": []any{}},
				map[string]any{"pid": 8, "bundle": "com.example.Locked", "name": "Locked", "windows": []any{}},
				map[string]any{"pid": 9, "bundle": "Vendor.Shell_abc123", "exe": "PWSH.exe", "name": "Shell", "windows": []any{}},
			}}
		case "snapshot":
			if req.Params["pid"] == float64(8) {
				refuse("computer.permission_missing", "accessibility")
			}
		case "request_permission":
			asked++
		case "screenshot":
			reply["result"] = map[string]any{
				"data": base64.StdEncoding.EncodeToString(fakeScreenshot()), "mime": "image/png",
				"bounds": map[string]any{"x": 100, "y": 50, "width": 2000, "height": 1000},
			}
		case "click":
			lastClick = req.Params
			reply["result"] = map[string]any{"role": "button"}
		case "press":
			refuse("computer.stale_ref", "a9 is gone")
		case "key":
			if req.Params["key"] == "Escape" {
				_ = enc.Encode(map[string]any{"event": "stop"})
				time.Sleep(20 * time.Millisecond)
			}
		case "_last_click":
			reply["result"] = lastClick
		case "_asked":
			reply["result"] = map[string]any{"asked": asked}
		case "menu", "hold_key", "paste", "pointer_move", "pointer_click", "pointer_drag":
			lastCall = map[string]any{"method": req.Method, "params": req.Params}
		case "scroll":
			lastCall = map[string]any{"method": req.Method, "params": req.Params}
			how := "wheel"
			if ref, _ := req.Params["ref"].(string); ref == "a2" {
				how = "revealed"
			}
			reply["result"] = map[string]any{"how": how}
		case "pointer_position":
			reply["result"] = map[string]any{"x": 600.0, "y": 300.0}
		case "pointer_release":
			released++
			reply["result"] = map[string]any{"returned": true}
		case "_last_call":
			reply["result"] = lastCall
		case "_released":
			reply["result"] = map[string]any{"count": released}
		case "_exit":
			os.Exit(0)
		}
		_ = enc.Encode(reply)
	}
}

func fakeSession(t *testing.T) *Session {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeHelperEnv, "1")
	return NewSession(NewHelper(exe))
}

func TestARefusedApplicationIsRefusedBeforeTheHelperIsAsked(t *testing.T) {
	s := NewSession(NewHelper("/nonexistent/helper"))
	for _, bundle := range []string{"com.apple.Terminal", " io.reasonix.studio ", "com.apple.systempreferences"} {
		if _, err := s.Act(context.Background(), bundle, []Step{{Action: "key", Key: "Enter"}}); CodeOf(err) != CodeAppRefused {
			t.Errorf("%q = %v, want %s", bundle, err, CodeAppRefused)
		}
	}
	if _, err := s.Apps(context.Background()); CodeOf(err) != CodeUnavailable {
		t.Fatalf("a helper that cannot start = %v, want %s", err, CodeUnavailable)
	}
}

// A packaged application is named by its package, and its executable can be
// one the list refuses under a package nobody listed.
func TestARefusedExecutableIsRefusedUnderAnyPackageName(t *testing.T) {
	s := fakeSession(t)
	if _, err := s.Snapshot(context.Background(), "Vendor.Shell_abc123"); CodeOf(err) != CodeAppRefused {
		t.Fatalf("a package whose executable is refused = %v, want %s", err, CodeAppRefused)
	}
}

func TestAClickAtAPointIsReadInTheLatestScreenshotsPixels(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	x, y := 100.0, 40.0
	click := []Step{{Action: "click", X: &x, Y: &y}}
	if _, err := s.Act(ctx, "com.example.Notes", click); CodeOf(err) != CodeNeedsScreenshot {
		t.Fatalf("a click before any screenshot = %v, want %s", err, CodeNeedsScreenshot)
	}
	shot, app, err := s.Screenshot(ctx, "com.example.Notes")
	if err != nil || app.PID != 7 {
		t.Fatalf("Screenshot = %v for %+v", err, app)
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(shot, "data:image/png;base64,"))
	fitted, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the screenshot the model sees does not decode: %v", err)
	}
	if _, _, err := visionimage.Fit(raw, "image/png"); err != nil || fitted.Width >= 4000 {
		t.Fatalf("the screenshot was not fitted for a vision model: %dpx, %v", fitted.Width, err)
	}
	res, err := s.Act(ctx, "com.example.Notes", click)
	if err != nil || res.Notes[0] != "click the button at (100,40)" {
		t.Fatalf("click = %+v, %v", res, err)
	}
	var got struct{ X, Y float64 }
	if err := s.helper.Call(ctx, "_last_click", nil, &got); err != nil {
		t.Fatal(err)
	}
	scale := 2000 / float64(fitted.Width)
	if got.X != 100+x*scale || got.Y != 50+y*scale {
		t.Fatalf("the helper was sent (%v,%v), want (%v,%v) in screen points", got.X, got.Y, 100+x*scale, 50+y*scale)
	}
}

func TestAMissingPermissionIsRequestedOnceAndSaysWhatThePersonDoes(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	for range 2 {
		_, err := s.Snapshot(ctx, "com.example.Locked")
		if CodeOf(err) != CodePermissionMissing || !strings.Contains(err.Error(), "Privacy & Security → Accessibility") {
			t.Fatalf("snapshot without accessibility = %v", err)
		}
	}
	var r struct{ Asked int }
	if err := s.helper.Call(ctx, "_asked", nil, &r); err != nil || r.Asked != 1 {
		t.Fatalf("the permission was requested %d times (%v), want once", r.Asked, err)
	}
}

func TestStepsStopAtTheFirstFailureAndWhenThePersonPressesEscape(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	res, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "key", Key: "Enter"}, {Action: "click", Ref: "a9"}, {Action: "key", Key: "Enter"}})
	if CodeOf(err) != CodeStaleRef || res.Done != 1 || res.FailedAt != 1 {
		t.Fatalf("a stale ref = %+v, %v", res, err)
	}
	res, err = s.Act(ctx, "com.example.Notes", []Step{{Action: "key", Key: "Escape"}, {Action: "key", Key: "Enter"}})
	if CodeOf(err) != CodeStopped || res.Done != 1 || res.FailedAt != 1 {
		t.Fatalf("Escape = %+v, %v", res, err)
	}
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "drag"}}); CodeOf(err) != CodeBadStep {
		t.Fatalf("an unknown action = %v, want %s", err, CodeBadStep)
	}
	if _, err := s.Act(ctx, "com.example.Missing", nil); CodeOf(err) != CodeNoApp {
		t.Fatalf("an application that is not running = %v, want %s", err, CodeNoApp)
	}
}

func TestAHelperThatExitsFailsWhatWasPendingAndStartsAgain(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	if err := s.helper.Call(ctx, "_exit", nil, nil); CodeOf(err) != CodeFailed {
		t.Fatalf("a call the helper died under = %v, want %s", err, CodeFailed)
	}
	if !(&Failure{Code: CodeFailed}).Is(&Failure{Code: CodeFailed, Detail: "other"}) {
		t.Fatal("failures with one code do not match")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		apps, err := s.Apps(ctx)
		if err == nil && len(apps) == 3 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the helper did not start again: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// fittedScale is how many screen points one pixel of the screenshot the model
// was given covers, which is what every point in a step is read in.
func fittedScale(t *testing.T, s *Session, ctx context.Context) float64 {
	t.Helper()
	shot, _, err := s.Screenshot(ctx, "com.example.Notes")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(shot, "data:image/png;base64,"))
	fitted, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return 2000 / float64(fitted.Width)
}

func lastCall(t *testing.T, s *Session) (string, map[string]any) {
	t.Helper()
	var got struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := s.helper.Call(context.Background(), "_last_call", nil, &got); err != nil {
		t.Fatal(err)
	}
	return got.Method, got.Params
}

func TestEveryPointerStepIsAimedInTheScreenshotsPixels(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	scale := fittedScale(t, s, ctx)
	x, y, toX, toY := 100.0, 40.0, 300.0, 120.0
	wantX, wantY := 100+x*scale, 50+y*scale

	res, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_move", X: &x, Y: &y}})
	if err != nil || res.Notes[0] != "move the pointer to (100,40)" {
		t.Fatalf("pointer_move = %+v, %v", res, err)
	}
	method, params := lastCall(t, s)
	if method != "pointer_move" || params["x"] != wantX || params["y"] != wantY {
		t.Fatalf("the helper was sent %s %v, want pointer_move at (%v,%v)", method, params, wantX, wantY)
	}

	res, err = s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_click", X: &x, Y: &y, Button: "right", Times: 2}})
	if err != nil || res.Notes[0] != "right click at (100,40)" {
		t.Fatalf("pointer_click = %+v, %v", res, err)
	}
	if method, params = lastCall(t, s); method != "pointer_click" || params["button"] != "right" || params["clicks"] != 2.0 {
		t.Fatalf("the helper was sent %s %v, want a right double click", method, params)
	}
	// A click that says neither button nor count is one left click.
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_click", X: &x, Y: &y}}); err != nil {
		t.Fatal(err)
	}
	if _, params = lastCall(t, s); params["button"] != "left" || params["clicks"] != 1.0 {
		t.Fatalf("a bare pointer_click sent %v, want one left click", params)
	}

	res, err = s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_drag", X: &x, Y: &y, ToX: &toX, ToY: &toY}})
	if err != nil || res.Notes[0] != "drag the pointer from (100,40) to (300,120)" {
		t.Fatalf("pointer_drag = %+v, %v", res, err)
	}
	method, params = lastCall(t, s)
	if method != "pointer_drag" || params["to_x"] != 100+toX*scale || params["to_y"] != 50+toY*scale {
		t.Fatalf("the helper was sent %s %v, want the end point in screen points", method, params)
	}
}

// The pointer is the person's, so a step that cannot say where it is going, or
// a drag that cannot say where it ends, is refused before the pointer moves.
func TestAPointerStepThatCannotSayWhereIsRefused(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	x := 100.0
	for _, step := range []Step{
		{Action: "pointer_move"},
		{Action: "pointer_drag", X: &x, Y: &x},
		{Action: "pointer_drag", X: &x, Y: &x, ToX: &x},
	} {
		if _, err := s.Act(ctx, "com.example.Notes", []Step{step}); CodeOf(err) != CodeNeedsScreenshot && CodeOf(err) != CodeBadStep {
			t.Fatalf("%+v = %v", step, err)
		}
	}
	_ = fittedScale(t, s, ctx)
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_drag", X: &x, Y: &x}}); CodeOf(err) != CodeBadStep {
		t.Fatalf("a drag with no destination = %v, want %s", err, CodeBadStep)
	}
}

// Taking the pointer borrows it: wherever the steps stop, it goes back.
func TestThePointerIsHandedBackWhicheverWayTheStepsEnd(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	_ = fittedScale(t, s, ctx)
	x, y := 100.0, 40.0
	released := func() float64 {
		var r struct{ Count float64 }
		if err := s.helper.Call(ctx, "_released", nil, &r); err != nil {
			t.Fatal(err)
		}
		return r.Count
	}
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "key", Key: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	if released() != 0 {
		t.Fatal("steps that never took the pointer gave one back")
	}
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_move", X: &x, Y: &y}}); err != nil {
		t.Fatal(err)
	}
	if released() != 1 {
		t.Fatal("the pointer was not given back after it was taken")
	}
	// A run that fails part way took it just the same.
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_move", X: &x, Y: &y}, {Action: "click", Ref: "a9"}}); CodeOf(err) != CodeStaleRef {
		t.Fatalf("want %s, got %v", CodeStaleRef, err)
	}
	if released() != 2 {
		t.Fatal("a run that failed part way kept the pointer")
	}
	if !PointerSteps([]Step{{Action: "key"}, {Action: "pointer_drag"}}) || PointerSteps([]Step{{Action: "click"}}) {
		t.Fatal("pointer steps are not recognised as the class needing the person's pointer")
	}
}

// Where the pointer is only means something in the frame the model was given.
func TestThePointerSaysWhereItIsInTheFrameTheModelHas(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	res, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_position"}})
	if err != nil || !strings.Contains(res.Notes[0], "take a screenshot") {
		t.Fatalf("pointer_position with no screenshot = %+v, %v", res, err)
	}
	scale := fittedScale(t, s, ctx)
	res, err = s.Act(ctx, "com.example.Notes", []Step{{Action: "pointer_position"}})
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("the pointer is at (%.0f,%.0f) in the screenshot", (600-100)/scale, (300-50)/scale)
	if res.Notes[0] != want {
		t.Fatalf("pointer_position = %q, want %q", res.Notes[0], want)
	}
}

func TestTheMenuBelongsToAnElementAndScrollingSaysWhichItWas(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "right_click"}}); CodeOf(err) != CodeBadStep {
		t.Fatalf("a right_click with no ref = %v, want %s", err, CodeBadStep)
	}
	res, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "right_click", Ref: "a4"}})
	if err != nil || res.Notes[0] != "open the menu of a4" {
		t.Fatalf("right_click = %+v, %v", res, err)
	}
	if method, params := lastCall(t, s); method != "menu" || params["ref"] != "a4" {
		t.Fatalf("the helper was sent %s %v, want the menu of a4", method, params)
	}
	// Bringing an element into view and turning the wheel are one step and two
	// outcomes, and the helper says which one it did.
	res, err = s.Act(ctx, "com.example.Notes", []Step{{Action: "scroll", Ref: "a2"}, {Action: "scroll", Amount: -3}})
	if err != nil || res.Notes[0] != "bring a2 into view" || res.Notes[1] != "scroll -3 lines" {
		t.Fatalf("scroll = %+v, %v", res, err)
	}
}

func TestHoldingAKeyAndRepeatingOneCarryTheirCount(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	res, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "hold_key", Key: "shift+a", Seconds: 1.5}})
	if err != nil || res.Notes[0] != "hold shift+a for 1.5s" {
		t.Fatalf("hold_key = %+v, %v", res, err)
	}
	if method, params := lastCall(t, s); method != "hold_key" || params["seconds"] != 1.5 {
		t.Fatalf("the helper was sent %s %v, want the seconds it holds for", method, params)
	}
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "key", Key: "Tab", Times: 4}}); err != nil {
		t.Fatal(err)
	}
}

func TestWaitingIsBoundedAndTheContextEndsIt(t *testing.T) {
	s := fakeSession(t)
	res, err := s.Act(context.Background(), "com.example.Notes", []Step{{Action: "wait", Ms: 5}})
	if err != nil || res.Notes[0] != "wait 5ms" {
		t.Fatalf("wait = %+v, %v", res, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "wait", Ms: 60000}}); err == nil {
		t.Fatal("a wait outlived the context that was cancelled under it")
	}
	// The same cancellation reaches a call already sent to the helper.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	if err := s.helper.Call(ctx2, "_silent", nil, nil); err == nil {
		t.Fatal("a call the helper never answered returned no error")
	}
}

func TestPastePutsTheWholeTextInAtOnce(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	res, err := s.Act(ctx, "com.example.Notes", []Step{{Action: "paste", Text: "从剪贴板 pasted"}})
	if err != nil || res.Notes[0] != "paste 11 characters" {
		t.Fatalf("paste = %+v, %v", res, err)
	}
	if method, params := lastCall(t, s); method != "paste" || params["text"] != "从剪贴板 pasted" {
		t.Fatalf("the helper was sent %s %v, want the text to paste", method, params)
	}
}
