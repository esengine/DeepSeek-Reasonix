package computer

import (
	"context"
	"errors"
	"testing"
)

func delivered(t *testing.T, s *Session) int {
	t.Helper()
	var r struct{ Count int }
	if err := s.helper.Call(context.Background(), "_delivered", nil, &r); err != nil {
		t.Fatal(err)
	}
	return r.Count
}

// Two independent windows make the target ambiguous: the call is refused before
// anything is sent, and the candidates come back with their ids and the front.
func TestAnApplicationWithTwoWindowsIsRefusedBeforeAnythingIsSent(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	for name, run := range map[string]func() error{
		"type": func() error {
			_, err := s.Act(ctx, "com.example.Two", 0, []Step{{Action: "type", Text: "x"}})
			return err
		},
		"key": func() error {
			_, err := s.Act(ctx, "com.example.Two", 0, []Step{{Action: "key", Key: "Enter"}})
			return err
		},
		"shot": func() error { _, _, err := s.Screenshot(ctx, "com.example.Two", 0); return err },
	} {
		err := run()
		f, ok := errors.AsType[*Failure](err)
		if !ok || f.Code != CodeAmbiguousWindow {
			t.Fatalf("%s = %v, want %s", name, err, CodeAmbiguousWindow)
		}
		if len(f.Candidates) != 2 || f.Candidates[0].ID != 101 || f.Candidates[1].ID != 102 {
			t.Errorf("%s: candidates = %+v", name, f.Candidates)
		}
	}
	if n := delivered(t, s); n != 0 {
		t.Fatalf("%d requests reached the helper before the refusal", n)
	}
}

// An element a ref names needs no window, and a window the model names settles
// the target and travels to the helper.
func TestANamedWindowOrARefSettlesTheTarget(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "com.example.Two", 0, []Step{{Action: "focus", Ref: "a1"}}); err != nil {
		t.Fatalf("a ref step on two windows = %v", err)
	}
	if res, err := s.Act(ctx, "com.example.Two", 102, []Step{{Action: "type", Text: "x"}}); err != nil || res.Done != 1 {
		t.Fatalf("type into window 102 = %+v, %v", res, err)
	}
	var sent struct {
		Method string
		Params map[string]any
	}
	if err := s.helper.Call(ctx, "_last_sent", nil, &sent); err != nil || sent.Method != "type" || sent.Params["window"] != float64(102) {
		t.Fatalf("the helper was sent %+v, %v", sent, err)
	}
	if _, _, err := s.Screenshot(ctx, "com.example.Two", 101); err != nil {
		t.Fatalf("screenshot of window 101 = %v", err)
	}
	if _, err := s.Act(ctx, "com.example.Two", 999, []Step{{Action: "type", Text: "x"}}); CodeOf(err) != CodeNoWindow {
		t.Fatalf("a window the application does not have = %v, want %s", err, CodeNoWindow)
	}
}

// A dialog another window owns is not a second window, and a helper that does
// not classify windows leaves the target unjudged.
func TestADialogOrAnUnclassifiedWindowIsNotAmbiguity(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	for _, bundle := range []string{"com.example.Dialog", "com.example.Flat"} {
		if _, err := s.Act(ctx, bundle, 0, []Step{{Action: "type", Text: "x"}}); err != nil {
			t.Errorf("%s: %v", bundle, err)
		}
	}
}

// A point read in one window's screenshot is not a point in another's.
func TestAPointNeedsTheScreenshotOfTheWindowItIsAimedAt(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	if _, _, err := s.Screenshot(ctx, "com.example.Two", 101); err != nil {
		t.Fatal(err)
	}
	x, y := 10.0, 10.0
	if _, err := s.Act(ctx, "com.example.Two", 102, []Step{{Action: "click", X: &x, Y: &y}}); CodeOf(err) != CodeNeedsScreenshot {
		t.Fatalf("a click aimed at a window never shot = %v, want %s", err, CodeNeedsScreenshot)
	}
	if _, err := s.Act(ctx, "com.example.Two", 101, []Step{{Action: "click", X: &x, Y: &y}}); err != nil {
		t.Fatalf("a click in the window that was shot = %v", err)
	}
}

// A capture of another window than the one asked for is not handed back as if
// it were: the helper may have aimed elsewhere, or the window may be gone.
func TestACaptureOfAnotherWindowIsRefused(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	if _, _, err := s.Screenshot(ctx, "com.example.Drift", 401); err != nil {
		t.Fatalf("the window that was captured = %v", err)
	}
	if _, _, err := s.Screenshot(ctx, "com.example.Drift", 402); CodeOf(err) != CodeNoWindow {
		t.Fatalf("a capture of window 401 for window 402 = %v, want %s", err, CodeNoWindow)
	}
}

// A helper that does not classify windows cannot aim at one, so naming a
// window there is refused rather than silently sent to the front one.
func TestANamedWindowIsRefusedWhereTheHelperCannotAimAtIt(t *testing.T) {
	s := fakeSession(t)
	ctx := context.Background()
	if _, err := s.Act(ctx, "com.example.Flat", 301, []Step{{Action: "type", Text: "x"}}); CodeOf(err) != CodeUnsupported {
		t.Fatalf("a window on an unclassified helper = %v, want %s", err, CodeUnsupported)
	}
	if _, _, err := s.Screenshot(ctx, "com.example.Flat", 301); CodeOf(err) != CodeUnsupported {
		t.Fatalf("a screenshot of a window on an unclassified helper = %v, want %s", err, CodeUnsupported)
	}
	if n := delivered(t, s); n != 0 {
		t.Fatalf("%d requests reached the helper", n)
	}
}

func TestTheAmbiguityNamesItsCandidatesBounded(t *testing.T) {
	f := &Failure{Code: CodeAmbiguousWindow, Detail: "d", Candidates: []Window{{ID: 5, Title: "A\nB"}, {ID: 6, Title: "C"}}}
	got := f.Error()
	want := `computer.ambiguous_window_target: d; window 5 "A\u{a}B" (front); window 6 "C"`
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
