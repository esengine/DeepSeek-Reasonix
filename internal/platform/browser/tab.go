package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const maxLogEntries = 300

// LogEntry is one thing a page reported: console output, an uncaught
// exception, or a request that failed.
type LogEntry struct {
	Seq   int64  `json:"seq"`
	Kind  string `json:"kind"`  // console | exception | network | download
	Level string `json:"level"` // error | warning | info
	Text  string `json:"text"`
}

// Dialog is a JavaScript dialog blocking the page.
type Dialog struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type tab struct {
	s         *Session
	eng       *engine
	id        string
	targetID  string
	sessionID string

	mu        sync.Mutex
	unsub     func()
	url       string
	title     string
	mainFrame string
	loaded    map[string]bool // loader ids whose load event has fired
	changed   chan struct{}   // closed and replaced on every load or navigation
	dialog    *Dialog
	logs      []LogEntry
	logSeq    int64
	requests  map[string]*Request
	netlog    []*Request
	popups    []string
	navigated int64   // main-frame cross-document navigations so far
	logRead   int64   // newest log seq a Logs call has returned
	shotScale float64 // CSS pixels per pixel of the latest screenshot
	pointer   pointerState
	runtime   runtimeState
	seen      seenSnapshot
}

type pointerState struct {
	x, y    float64
	visible bool
}

type runtimeState struct {
	contexts map[string]string
}

// seenSnapshot is the last whole-page snapshot the agent was given, and which
// document it was of.
type seenSnapshot struct {
	lines    []string
	document int64
	valid    bool
}

func newTab(s *Session, eng *engine, id, targetID, sessionID string) *tab {
	t := &tab{
		s: s, eng: eng, id: id, targetID: targetID, sessionID: sessionID,
		loaded: map[string]bool{}, changed: make(chan struct{}), requests: map[string]*Request{},
		runtime: runtimeState{contexts: map[string]string{}},
	}
	t.unsub = eng.conn.subscribe(sessionID, t.onEvent)
	return t
}

// call sends a command to this tab. A JavaScript dialog stops the page from
// answering anything that runs on it, so such a command fails at once while a
// dialog is open and is abandoned when one opens while it waits.
func (t *tab) call(ctx context.Context, method string, params, out any) error {
	input := strings.HasPrefix(method, "Input.")
	if !input && !waitsOnPage(method) {
		return t.eng.conn.call(ctx, t.sessionID, method, params, out)
	}
	t.mu.Lock()
	dialog, changed := t.dialog, t.changed
	t.mu.Unlock()
	if dialog != nil {
		return dialogFailure(dialog)
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watching := make(chan struct{})
	defer close(watching)
	go func() {
		for {
			select {
			case <-changed:
			case <-watching:
				return
			}
			t.mu.Lock()
			open := t.dialog != nil
			changed = t.changed
			t.mu.Unlock()
			if open {
				cancel()
				return
			}
		}
	}()
	err := t.eng.conn.call(callCtx, t.sessionID, method, params, out)
	if err != nil && ctx.Err() == nil && callCtx.Err() != nil {
		if d := t.currentDialog(); d != nil {
			if input {
				return nil // the input was delivered; the dialog is what it did
			}
			return dialogFailure(d)
		}
	}
	return err
}

// waitsOnPage reports whether a command is answered by the page itself rather
// than by the browser around it.
func waitsOnPage(method string) bool {
	for _, prefix := range []string{"Runtime.", "DOM.", "Accessibility.", "Page.captureScreenshot", "Page.getLayoutMetrics"} {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	return false
}

func dialogFailure(d *Dialog) *Failure {
	return &Failure{Code: CodeDialogOpen, Detail: fmt.Sprintf("a %s dialog is open (%q); answer it with a dialog step first", d.Type, d.Message)}
}

func (t *tab) enable(ctx context.Context) error {
	for _, method := range []string{"Page.enable", "Runtime.enable", "Network.enable"} {
		if err := t.call(ctx, method, nil, nil); err != nil {
			return err
		}
	}
	if err := t.call(ctx, "Page.setLifecycleEventsEnabled", map[string]any{"enabled": true}, nil); err != nil {
		return err
	}
	var tree struct {
		FrameTree struct {
			Frame struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := t.call(ctx, "Page.getFrameTree", nil, &tree); err != nil {
		return err
	}
	t.mu.Lock()
	t.mainFrame, t.url = tree.FrameTree.Frame.ID, tree.FrameTree.Frame.URL
	t.mu.Unlock()
	return nil
}

func (t *tab) detach() {
	t.mu.Lock()
	unsub := t.unsub
	t.unsub = nil
	t.mu.Unlock()
	if unsub != nil {
		unsub()
	}
}

func (t *tab) location() (url, title string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.url, t.title
}

// info reads the title fresh when asked to, since pages set it after load.
func (t *tab) info(refreshTitle bool) TabInfo {
	if refreshTitle {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		var r struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if t.call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.title", "returnByValue": true}, &r) == nil {
			t.mu.Lock()
			retitled := t.title != r.Result.Value
			t.title = r.Result.Value
			t.mu.Unlock()
			if retitled {
				t.s.tabsChanged()
			}
		}
		cancel()
	}
	url, title := t.location()
	return TabInfo{ID: t.id, Target: t.targetID, URL: url, Title: title, Active: t.s.activeTab() == t}
}

func (t *tab) signalLocked() {
	close(t.changed)
	t.changed = make(chan struct{})
}

func (t *tab) onEvent(ev event) {
	if t.onRuntimeEvent(ev) {
		return
	}
	switch ev.Method {
	case "Page.frameNavigated":
		var p struct {
			Frame struct {
				ID          string `json:"id"`
				ParentID    string `json:"parentId"`
				URL         string `json:"url"`
				URLFragment string `json:"urlFragment"`
			} `json:"frame"`
		}
		if json.Unmarshal(ev.Params, &p) != nil || p.Frame.ParentID != "" {
			return
		}
		t.mu.Lock()
		delete(t.runtime.contexts, t.mainFrame)
		t.mainFrame, t.url = p.Frame.ID, p.Frame.URL+p.Frame.URLFragment
		t.navigated++
		t.pointer = pointerState{}
		t.signalLocked()
		t.mu.Unlock()
		t.s.refs.retireTab(t.id)
		t.s.tabsChanged()
	case "Page.navigatedWithinDocument":
		var p struct {
			FrameID string `json:"frameId"`
			URL     string `json:"url"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		t.mu.Lock()
		if p.FrameID == t.mainFrame {
			t.url = p.URL
			t.signalLocked()
		}
		t.mu.Unlock()
	case "Page.lifecycleEvent":
		var p struct {
			FrameID  string `json:"frameId"`
			LoaderID string `json:"loaderId"`
			Name     string `json:"name"`
		}
		if json.Unmarshal(ev.Params, &p) != nil || p.Name != "load" {
			return
		}
		t.mu.Lock()
		main := p.FrameID == t.mainFrame
		if main {
			t.loaded[p.LoaderID] = true
			t.signalLocked()
		}
		t.mu.Unlock()
		if main {
			t.s.tabsChanged()
		}
	case "Page.javascriptDialogOpening":
		var d Dialog
		if json.Unmarshal(ev.Params, &d) != nil {
			return
		}
		t.mu.Lock()
		t.dialog = &d
		t.signalLocked()
		t.mu.Unlock()
	case "Page.javascriptDialogClosed":
		t.mu.Lock()
		t.dialog = nil
		t.signalLocked()
		t.mu.Unlock()
	case "Runtime.consoleAPICalled":
		t.onConsole(ev.Params)
	case "Runtime.exceptionThrown":
		var p struct {
			ExceptionDetails struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
				Stack stack `json:"stackTrace"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(ev.Params, &p) != nil || p.ExceptionDetails.Stack.hostInjected() {
			return
		}
		text := p.ExceptionDetails.Exception.Description
		if text == "" {
			text = p.ExceptionDetails.Text
		}
		t.log("exception", "error", text)
	default:
		t.onNetwork(ev)
	}
}

func (t *tab) onRuntimeEvent(ev event) bool {
	switch ev.Method {
	case "Runtime.executionContextCreated":
		var p struct {
			Context struct {
				UniqueID string `json:"uniqueId"`
				AuxData  struct {
					FrameID   string `json:"frameId"`
					IsDefault bool   `json:"isDefault"`
				} `json:"auxData"`
			} `json:"context"`
		}
		if json.Unmarshal(ev.Params, &p) == nil && p.Context.AuxData.IsDefault && p.Context.UniqueID != "" {
			t.mu.Lock()
			t.runtime.contexts[p.Context.AuxData.FrameID] = p.Context.UniqueID
			t.mu.Unlock()
		}
	case "Runtime.executionContextDestroyed":
		var p struct {
			UniqueID string `json:"executionContextUniqueId"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			t.mu.Lock()
			for frame, id := range t.runtime.contexts {
				if id == p.UniqueID {
					delete(t.runtime.contexts, frame)
				}
			}
			t.mu.Unlock()
		}
	case "Runtime.executionContextsCleared":
		t.mu.Lock()
		clear(t.runtime.contexts)
		t.mu.Unlock()
	default:
		return false
	}
	return true
}

// stack is as much of a console message's origin as this needs: which script
// each frame ran from.
type stack struct {
	Frames []struct {
		URL string `json:"url"`
	} `json:"callFrames"`
}

// hostInjected reports a message no script of the page produced. The window
// this browser runs in injects its own bundle into every page and warns there
// about the page's security headers; the model reads that as the page speaking.
// A frame from an extension or from devtools is the same kind of visitor. A
// message with no stack at all stays: unattributed is not the host's.
func (s stack) hostInjected() bool {
	if len(s.Frames) == 0 {
		return false
	}
	for _, f := range s.Frames {
		scheme, _, ok := strings.Cut(f.URL, ":")
		if !ok || !injectedSchemes[strings.ToLower(scheme)] {
			return false
		}
	}
	return true
}

var injectedSchemes = map[string]bool{"node": true, "chrome-extension": true, "devtools": true, "chrome": true}

// onNetwork records what a page asked the network for, and reports the answers
// a person would want to hear about without being asked.
func (t *tab) onNetwork(ev event) {
	switch ev.Method {
	case "Network.requestWillBeSent":
		var p struct {
			RequestID string  `json:"requestId"`
			Type      string  `json:"type"`
			Timestamp float64 `json:"timestamp"`
			Request   struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"request"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		t.requestStarted(p.RequestID, p.Request.Method, p.Request.URL, strings.ToLower(p.Type), p.Timestamp)
	case "Network.responseReceived":
		var p struct {
			RequestID string `json:"requestId"`
			Response  struct {
				URL      string `json:"url"`
				Status   int    `json:"status"`
				MimeType string `json:"mimeType"`
			} `json:"response"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		t.requestResponded(p.RequestID, p.Response.Status, p.Response.MimeType)
		if p.Response.Status >= 400 {
			t.log("network", "warning", fmt.Sprintf("%s → HTTP %d", t.requestName(p.RequestID, p.Response.URL), p.Response.Status))
		}
	case "Network.loadingFailed":
		var p struct {
			RequestID string  `json:"requestId"`
			ErrorText string  `json:"errorText"`
			Canceled  bool    `json:"canceled"`
			Timestamp float64 `json:"timestamp"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			return
		}
		name := t.requestName(p.RequestID, "")
		t.requestEnded(p.RequestID, p.Timestamp, 0, p.ErrorText)
		if !p.Canceled {
			t.log("network", "error", fmt.Sprintf("%s failed: %s", name, p.ErrorText))
		}
	case "Network.loadingFinished":
		var p struct {
			RequestID         string  `json:"requestId"`
			Timestamp         float64 `json:"timestamp"`
			EncodedDataLength float64 `json:"encodedDataLength"`
		}
		if json.Unmarshal(ev.Params, &p) == nil {
			t.requestEnded(p.RequestID, p.Timestamp, int64(p.EncodedDataLength), "")
		}
	}
}

func (t *tab) onConsole(params json.RawMessage) {
	var p struct {
		Type string `json:"type"`
		Args []struct {
			Type        string          `json:"type"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"args"`
		Stack stack `json:"stackTrace"`
	}
	if json.Unmarshal(params, &p) != nil || p.Stack.hostInjected() {
		return
	}
	parts := make([]string, 0, len(p.Args))
	for _, a := range p.Args {
		var s string
		switch {
		case a.Type == "string" && json.Unmarshal(a.Value, &s) == nil:
			parts = append(parts, s)
		case len(a.Value) > 0:
			parts = append(parts, string(a.Value))
		default:
			parts = append(parts, a.Description)
		}
	}
	level := "info"
	switch p.Type {
	case "error", "assert":
		level = "error"
	case "warning":
		level = "warning"
	}
	t.log("console", level, strings.Join(parts, " "))
}

func (t *tab) log(kind, level, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logSeq++
	t.logs = append(t.logs, LogEntry{Seq: t.logSeq, Kind: kind, Level: level, Text: text})
	if n := len(t.logs); n > maxLogEntries {
		t.logs = append([]LogEntry(nil), t.logs[n-maxLogEntries:]...)
	}
}

// logsAfter returns entries newer than seq, and the newest seq it has seen.
func (t *tab) logsAfter(seq int64) ([]LogEntry, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []LogEntry
	for _, e := range t.logs {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out, t.logSeq
}

func (t *tab) notePopup(id string) {
	t.mu.Lock()
	t.popups = append(t.popups, id)
	t.mu.Unlock()
}

func (t *tab) currentDialog() *Dialog {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dialog == nil {
		return nil
	}
	d := *t.dialog
	return &d
}

// navigate loads url in this tab and waits for its load event.
func (t *tab) navigate(ctx context.Context, url string) error {
	loaderID, err := t.start(ctx, url)
	if err != nil || loaderID == "" {
		return err
	}
	return t.waitLoaded(ctx, loaderID, navigationTimeout)
}

// start begins a navigation and returns once the page has committed: the
// document is arriving and the view draws it, though its subresources may
// never finish. The loader id is empty for a navigation within the document.
func (t *tab) start(ctx context.Context, url string) (string, error) {
	var r struct {
		LoaderID  string `json:"loaderId"`
		ErrorText string `json:"errorText"`
	}
	if err := t.call(ctx, "Page.navigate", map[string]any{"url": url}, &r); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fail(CodeNavigationTimeout, "%s did not start answering within %s", url, navigationTimeout)
		}
		return "", engineFailure(err)
	}
	if r.ErrorText != "" {
		return "", fail(CodeNavigationFailed, "%s: %s", url, r.ErrorText)
	}
	return r.LoaderID, nil
}

func (t *tab) waitLoaded(ctx context.Context, loaderID string, limit time.Duration) error {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for {
		t.mu.Lock()
		done, changed, dialog := t.loaded[loaderID], t.changed, t.dialog
		t.mu.Unlock()
		if done {
			return nil
		}
		if dialog != nil {
			return dialogFailure(dialog)
		}
		select {
		case <-changed:
		case <-deadline.C:
			return fail(CodeNavigationTimeout, "the page did not finish loading within %s; it may still be usable", limit)
		case <-ctx.Done():
			return ctx.Err()
		case <-t.eng.conn.closed():
			return engineFailure(errConnClosed)
		}
	}
}

// settle waits for a navigation an input may have started. Nothing starting
// within the grace period is the common case and not a failure.
func (t *tab) settle(ctx context.Context, before int64) {
	grace := time.NewTimer(150 * time.Millisecond)
	defer grace.Stop()
	for {
		t.mu.Lock()
		navigated, changed := t.navigated > before, t.changed
		t.mu.Unlock()
		if navigated {
			_ = t.waitAnyLoad(ctx, navigationTimeout)
			return
		}
		select {
		case <-changed:
		case <-grace.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// waitAnyLoad waits until the document the main frame is now showing has loaded.
func (t *tab) waitAnyLoad(ctx context.Context, limit time.Duration) error {
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for {
		if t.call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true}, &r) == nil && r.Result.Value == "complete" {
			return nil
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			return fail(CodeNavigationTimeout, "the page did not finish loading within %s", limit)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (t *tab) navigationCount() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.navigated
}

// checkCurrentURL refuses a page the agent could not have opened itself, which
// a link from an allowed local file can still reach. The browser's own error
// page is what a failed load leaves behind and stays readable.
func (t *tab) checkCurrentURL() error {
	url, _ := t.location()
	if url == "" || strings.HasPrefix(url, "about:") || strings.HasPrefix(url, "chrome-error:") {
		return nil
	}
	_, err := checkURL(url, t.s.cfg.Roots)
	return err
}
