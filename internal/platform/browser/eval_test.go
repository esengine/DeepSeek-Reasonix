package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestEvalStepRunsPageJavaScriptAndReturnsItsValue(t *testing.T) {
	page := &fakePage{evalResult: map[string]any{"changed": true}}
	s, tb := newFakeSession(t, page)
	tb.url, tb.mainFrame = "https://example.com/page", "f1"
	tb.runtime.contexts["f1"] = "context-1"
	res, err := s.Act(context.Background(), "", []Step{{Action: "eval", Script: `document.body.dataset.ready = "yes"; ({changed: true})`}}, false, "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if page.evalExpression == "" || page.evalContext != "context-1" || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], `"changed":true`) {
		t.Fatalf("expression = %q, result = %+v", page.evalExpression, res)
	}
}

func TestEvalStepPreservesScriptFailureIdentity(t *testing.T) {
	page := &fakePage{evalException: "Error: no button"}
	s, tb := newFakeSession(t, page)
	tb.url, tb.mainFrame = "https://example.com", "f1"
	tb.runtime.contexts["f1"] = "context-1"
	res, err := s.Act(context.Background(), "", []Step{{Action: "eval", Script: `throw new Error("no button")`}}, false, "https://example.com")
	if CodeOf(err) != CodeScriptFailed || res.FailedAt != 0 {
		t.Fatalf("Act = %+v, %v; want %s at step 0", res, err, CodeScriptFailed)
	}
}

func TestEvalStepRefusesAnOriginChangedAfterApproval(t *testing.T) {
	page := &fakePage{evalResult: true}
	s, tb := newFakeSession(t, page)
	tb.url, tb.mainFrame = "https://other.example", "f1"
	tb.runtime.contexts["f1"] = "context-1"
	res, err := s.Act(context.Background(), "", []Step{{Action: "eval", Script: `location.href`}}, false, "https://approved.example")
	if CodeOf(err) != CodeOriginChanged || res.FailedAt != 0 || page.evalExpression != "" {
		t.Fatalf("Act = %+v, %v, expression %q; want %s before evaluation", res, err, page.evalExpression, CodeOriginChanged)
	}
}

func TestEvaluateReturnsUnserializableValue(t *testing.T) {
	page := &fakePage{evalRemote: map[string]any{"type": "number", "unserializableValue": "NaN"}}
	s, tb := newFakeSession(t, page)
	tb.url, tb.mainFrame = "https://example.com", "f1"
	tb.runtime.contexts["f1"] = "context-1"
	got, _, err := s.Evaluate(context.Background(), "", `NaN`, "https://example.com")
	if err != nil || got != "NaN" {
		t.Fatalf("Evaluate = %q, %v; want NaN", got, err)
	}
}

func TestEvaluatePreservesStringResult(t *testing.T) {
	for _, want := range []string{"", "  value  "} {
		page := &fakePage{evalRemote: map[string]any{"type": "string", "value": want}}
		s, tb := newFakeSession(t, page)
		tb.url, tb.mainFrame = "https://example.com", "f1"
		tb.runtime.contexts["f1"] = "context-1"
		got, _, err := s.Evaluate(context.Background(), "", `"value"`, "https://example.com")
		if err != nil || got != want {
			t.Fatalf("Evaluate = %q, %v; want %q", got, err, want)
		}
	}
}

func TestEvaluateReportsContextDestroyedDuringCallAsOriginChanged(t *testing.T) {
	var tb *tab
	ft := newFakeTransport(func(msg wireMessage) []byte {
		if msg.Method == "Runtime.evaluate" {
			tb.onEvent(event{Method: "Runtime.executionContextDestroyed", Params: json.RawMessage(`{"executionContextUniqueId":"context-1"}`)})
			raw, _ := json.Marshal(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32000, "message": "context unavailable"}})
			return raw
		}
		raw, _ := json.Marshal(map[string]any{"id": msg.ID, "result": map[string]any{}})
		return raw
	})
	s := NewSession(Config{})
	eng := &engine{conn: newConn(ft), exited: make(chan struct{}), listeners: map[int]func(event){}}
	tb = newTab(s, eng, "t1", "target-1", "S1")
	tb.url, tb.mainFrame = "https://example.com", "f1"
	tb.runtime.contexts["f1"] = "context-1"
	s.eng, s.tabs, s.active = eng, []*tab{tb}, tb
	t.Cleanup(func() { eng.conn.shutdown() })
	_, _, err := s.Evaluate(context.Background(), "", `location.href`, "https://example.com")
	if CodeOf(err) != CodeOriginChanged {
		t.Fatalf("Evaluate error = %v; want %s", err, CodeOriginChanged)
	}
}

func TestPointerIsHiddenOnNavigationAndResize(t *testing.T) {
	page := &fakePage{}
	_, tb := newFakeSession(t, page)
	tb.pointer = pointerState{x: 20, y: 30, visible: true}
	tb.onEvent(event{Method: "Page.frameNavigated", Params: json.RawMessage(`{"frame":{"id":"f2","url":"https://example.com"}}`)})
	if tb.pointer.visible {
		t.Fatal("pointer stayed visible after main-frame navigation")
	}
	tb.pointer = pointerState{x: 20, y: 30, visible: true}
	if _, err := tb.resize(context.Background(), Step{Width: 800, Height: 600}); err != nil {
		t.Fatal(err)
	}
	if tb.pointer.visible {
		t.Fatal("pointer stayed visible after resize")
	}
}

func TestClickDoesNotRestorePointerAfterNavigation(t *testing.T) {
	var tb *tab
	ft := newFakeTransport(func(msg wireMessage) []byte {
		if msg.Method == "Input.dispatchMouseEvent" {
			var p struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if p.Type == "mousePressed" {
				tb.onEvent(event{Method: "Page.frameNavigated", Params: json.RawMessage(`{"frame":{"id":"f2","url":"https://next.example"}}`)})
			}
		}
		raw, _ := json.Marshal(map[string]any{"id": msg.ID, "result": map[string]any{}})
		return raw
	})
	s := NewSession(Config{})
	eng := &engine{conn: newConn(ft), exited: make(chan struct{}), listeners: map[int]func(event){}}
	tb = newTab(s, eng, "t1", "target-1", "S1")
	s.eng, s.tabs, s.active = eng, []*tab{tb}, tb
	t.Cleanup(func() { eng.conn.shutdown() })
	if err := tb.click(context.Background(), 20, 30, 1); err != nil {
		t.Fatal(err)
	}
	if tb.pointer.visible {
		t.Fatal("click restored its old pointer after navigation")
	}
}

func TestScreenshotPointerMarkerIsVisible(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := range 40 {
		for x := range 40 {
			src.Set(x, y, color.RGBA{128, 128, 128, 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	marked, err := markPointer(raw.Bytes(), 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := image.Decode(bytes.NewReader(marked))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := got.At(20, 20).RGBA()
	if r < 0xd000 || g < 0xd000 || b < 0xd000 {
		t.Fatalf("marker center = %#04x %#04x %#04x, want a visible light center", r, g, b)
	}
}

func TestEvaluateGivesUpOnAScriptWhosePromiseNeverSettles(t *testing.T) {
	prev := evalTimeout
	evalTimeout = 150 * time.Millisecond
	t.Cleanup(func() { evalTimeout = prev })
	var calls []string
	ft := newFakeTransport(func(msg wireMessage) []byte {
		calls = append(calls, msg.Method)
		if msg.Method == "Runtime.evaluate" && !strings.Contains(string(msg.Params), "document.title") {
			return nil
		}
		raw, _ := json.Marshal(map[string]any{"id": msg.ID, "result": map[string]any{}})
		return raw
	})
	s := NewSession(Config{})
	eng := &engine{conn: newConn(ft), exited: make(chan struct{}), listeners: map[int]func(event){}}
	tb := newTab(s, eng, "t1", "target-1", "S1")
	tb.url, tb.mainFrame = "https://example.com", "f1"
	tb.runtime.contexts["f1"] = "context-1"
	s.eng, s.tabs, s.active = eng, []*tab{tb}, tb
	t.Cleanup(func() { eng.conn.shutdown() })

	caller, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, _, err := s.Evaluate(caller, "", `await new Promise(() => {})`, "https://example.com")
	if CodeOf(err) != CodeScriptTimeout {
		t.Fatalf("Evaluate error = %v; want %s", err, CodeScriptTimeout)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("Evaluate took %s; the limit is %s", time.Since(start), evalTimeout)
	}
	if !slices.Contains(calls, "Runtime.terminateExecution") {
		t.Fatalf("calls = %v; want the script's execution terminated", calls)
	}
}

func TestEvaluateKeepsTheCallersOwnCancellationDistinctFromTheScriptLimit(t *testing.T) {
	ft := newFakeTransport(func(msg wireMessage) []byte {
		if msg.Method == "Runtime.evaluate" && !strings.Contains(string(msg.Params), "document.title") {
			return nil
		}
		raw, _ := json.Marshal(map[string]any{"id": msg.ID, "result": map[string]any{}})
		return raw
	})
	s := NewSession(Config{})
	eng := &engine{conn: newConn(ft), exited: make(chan struct{}), listeners: map[int]func(event){}}
	tb := newTab(s, eng, "t1", "target-1", "S1")
	tb.url, tb.mainFrame = "https://example.com", "f1"
	tb.runtime.contexts["f1"] = "context-1"
	s.eng, s.tabs, s.active = eng, []*tab{tb}, tb
	t.Cleanup(func() { eng.conn.shutdown() })

	caller, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, _, err := s.Evaluate(caller, "", `await new Promise(() => {})`, "https://example.com")
	if err == nil || CodeOf(err) == CodeScriptTimeout {
		t.Fatalf("Evaluate error = %v; a stop by the caller is not the script limit", err)
	}
}

func TestEvaluateTruncatesALongResultOnACharacterBoundary(t *testing.T) {
	// 30,000 three-byte characters stay under the in-page limit (counted in UTF-16
	// units) but exceed maxEvalResult bytes, and byte maxEvalResult falls inside a
	// character.
	long := strings.Repeat("测", 30000)
	page := &fakePage{evalRemote: map[string]any{"type": "string", "value": long}}
	s, tb := newFakeSession(t, page)
	tb.url, tb.mainFrame = "https://example.com", "f1"
	tb.runtime.contexts["f1"] = "context-1"
	got, _, err := s.Evaluate(context.Background(), "", `"x".repeat(1)`, "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("Evaluate split a character: %q", got[len(got)-40:])
	}
	kept, ok := strings.CutSuffix(got, "\n… result truncated")
	if !ok || !strings.HasPrefix(long, kept) {
		t.Fatalf("Evaluate = …%q, want a prefix of the result and the truncation note", got[len(got)-40:])
	}
	if len(kept) > maxEvalResult || len(kept) < maxEvalResult-3 {
		t.Fatalf("Evaluate kept %d bytes, want just under %d", len(kept), maxEvalResult)
	}
}
