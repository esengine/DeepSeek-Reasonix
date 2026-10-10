package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	objectGroup    = "reasonix"
	maxWait        = 30 * time.Second
	defaultWaitFor = 5 * time.Second
)

// Step is one input. X and Y are in the pixels of the tab's latest screenshot.
// dragSteps is how many moves a drag is split into: a page that follows the
// pointer needs to see it travel, not jump.
const dragSteps = 8

type Step struct {
	Action string   `json:"action"`
	Ref    string   `json:"ref,omitempty"`
	Text   string   `json:"text,omitempty"`
	Script string   `json:"script,omitempty"`
	Key    string   `json:"key,omitempty"`
	Values []string `json:"values,omitempty"`
	X      *float64 `json:"x,omitempty"`
	Y      *float64 `json:"y,omitempty"`
	DeltaX float64  `json:"delta_x,omitempty"`
	DeltaY float64  `json:"delta_y,omitempty"`
	// Where a drag ends: another element, or a point in the page.
	ToRef string   `json:"to_ref,omitempty"`
	ToX   *float64 `json:"to_x,omitempty"`
	ToY   *float64 `json:"to_y,omitempty"`
	Files []string `json:"files,omitempty"`
	// The viewport a resize asks for, in CSS pixels.
	Width  int   `json:"width,omitempty"`
	Height int   `json:"height,omitempty"`
	Ms     int   `json:"ms,omitempty"`
	Accept *bool `json:"accept,omitempty"`
}

// ActResult is what a run of steps did before it finished or stopped.
type ActResult struct {
	Tab      TabInfo
	Done     int        // steps that completed
	Notes    []string   // one per completed step
	Logs     []LogEntry // errors and warnings the page reported meanwhile
	Opened   []string   // tabs the page opened meanwhile
	Dialog   *Dialog
	FailedAt int // index of the step that failed, or -1
}

// Act runs steps in order on a tab and stops at the first that fails. The
// returned error is that step's failure; the result says what came before it.
// secrets says the call was confirmed as entering one (see CredentialEntry);
// without it a step that types into a secret field is refused.
func (s *Session) Act(ctx context.Context, tabID string, steps []Step, secrets bool, scriptOrigin string) (ActResult, error) {
	t, err := s.tab(tabID)
	if err != nil {
		return ActResult{FailedAt: 0}, err
	}
	if err := t.checkCurrentURL(); err != nil {
		return ActResult{FailedAt: 0}, err
	}
	_, logMark := t.logsAfter(1 << 62)
	t.mu.Lock()
	popupMark := len(t.popups)
	t.mu.Unlock()
	res := ActResult{FailedAt: -1}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		_ = t.call(releaseCtx, "Runtime.releaseObjectGroup", map[string]any{"objectGroup": objectGroup}, nil)
		cancel()
	}()
	var stepErr error
	for i, step := range steps {
		note, err := s.runStep(ctx, t, step, secrets, scriptOrigin)
		if err != nil {
			res.FailedAt, stepErr = i, err
			break
		}
		res.Done++
		res.Notes = append(res.Notes, note)
	}
	entries, _ := t.logsAfter(logMark)
	for _, e := range entries {
		if e.Level != "info" {
			res.Logs = append(res.Logs, e)
		}
	}
	t.mu.Lock()
	res.Opened = append(res.Opened, t.popups[popupMark:]...)
	t.mu.Unlock()
	active := s.activeTab()
	if active == nil {
		active = t
	}
	res.Tab = active.info(true)
	res.Dialog = active.currentDialog()
	return res, stepErr
}

func (s *Session) runStep(ctx context.Context, t *tab, step Step, secrets bool, scriptOrigin string) (string, error) {
	action := strings.ToLower(strings.TrimSpace(step.Action))
	if d := t.currentDialog(); d != nil && action != "dialog" {
		return "", dialogFailure(d)
	}
	before := t.navigationCount()
	switch action {
	case "click", "double_click":
		return s.clickStep(ctx, t, step, action, before)
	case "hover":
		x, y, err := s.point(ctx, t, step)
		if err != nil {
			return "", err
		}
		if err := t.mouse(ctx, "mouseMoved", x, y, 0); err != nil {
			return "", err
		}
		return "hover " + target(step), nil
	case "type", "fill":
		return s.enterText(ctx, t, step, action == "fill", secrets)
	case "press":
		if err := t.press(ctx, step.Key); err != nil {
			return "", err
		}
		t.settle(ctx, before)
		return "press " + step.Key, nil
	case "select":
		return s.selectOptions(ctx, t, step)
	case "scroll":
		x, y, err := s.scrollPoint(ctx, t, step)
		if err != nil {
			return "", err
		}
		if err := t.call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseWheel", "x": x, "y": y, "deltaX": step.DeltaX, "deltaY": step.DeltaY}, nil); err != nil {
			return "", engineFailure(err)
		}
		if step.DeltaX != 0 && step.DeltaY == 0 {
			return fmt.Sprintf("scroll %v px sideways", step.DeltaX), nil
		}
		return fmt.Sprintf("scroll %v px", step.DeltaY), nil
	case "wait":
		d := min(time.Duration(step.Ms)*time.Millisecond, maxWait)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return fmt.Sprintf("wait %s", d), nil
	case "wait_for":
		return t.waitForText(ctx, step)
	case "dialog":
		return t.answerDialog(ctx, step)
	case "eval":
		return s.evalStep(ctx, t, step, scriptOrigin)
	case "back":
		return t.history(ctx, -1)
	case "forward":
		return t.history(ctx, 1)
	case "reload":
		return t.reload(ctx)
	case "drag":
		return s.drag(ctx, t, step)
	case "upload":
		return s.upload(ctx, t, step)
	case "resize":
		return t.resize(ctx, step)
	}
	return "", fail(CodeBadStep, "unknown action %q", step.Action)
}

func (s *Session) clickStep(ctx context.Context, t *tab, step Step, action string, before int64) (string, error) {
	x, y, err := s.point(ctx, t, step)
	if err != nil {
		return "", err
	}
	clicks := 1
	if action == "double_click" {
		clicks = 2
	}
	if err := t.click(ctx, x, y, clicks); err != nil {
		return "", err
	}
	t.settle(ctx, before)
	return fmt.Sprintf("%s %s", action, target(step)), nil
}

func (s *Session) evalStep(ctx context.Context, t *tab, step Step, approvedOrigin string) (string, error) {
	value, _, err := s.Evaluate(ctx, t.id, step.Script, approvedOrigin)
	if err != nil {
		return "", err
	}
	return "eval => " + value, nil
}

// enterText types step.Text into its ref, or where focus is; fill replaces the
// field's value instead of inserting at the cursor.
func (s *Session) enterText(ctx context.Context, t *tab, step Step, fill, secrets bool) (string, error) {
	if !secrets && s.holdsSecret(ctx, t, step.Ref) {
		return "", &Failure{Code: CodeUnconfirmedSecret, Ref: step.Ref, Detail: "this step types into a field that holds a secret (a password, a one-time code or card details), which was not confirmed for this call; send it again in a browser_act of its own that names the field by ref"}
	}
	if step.Ref != "" {
		if err := s.focus(ctx, t, step.Ref, fill); err != nil {
			return "", err
		}
	}
	if step.Text == "" && fill {
		if err := t.press(ctx, "Backspace"); err != nil {
			return "", err
		}
	} else if err := t.call(ctx, "Input.insertText", map[string]any{"text": step.Text}, nil); err != nil {
		return "", engineFailure(err)
	}
	return fmt.Sprintf("%s %d characters into %s", step.Action, len([]rune(step.Text)), target(step)), nil
}

func target(step Step) string {
	if step.Ref != "" {
		return step.Ref
	}
	if step.X != nil && step.Y != nil {
		return fmt.Sprintf("(%v,%v)", *step.X, *step.Y)
	}
	return "the focused element"
}

// node resolves a ref on t to a backend node that still exists.
func (s *Session) node(ctx context.Context, t *tab, ref string) (int64, error) {
	target, err := s.refs.resolve(ref)
	if err != nil {
		return 0, err
	}
	if target.tab != t.id {
		return 0, &Failure{Code: CodeUnknownRef, Ref: ref, Detail: fmt.Sprintf("%s is on tab %s; switch to it first", ref, target.tab)}
	}
	if err := t.call(ctx, "DOM.describeNode", map[string]any{"backendNodeId": target.node}, nil); err != nil {
		if isProtocolError(err) {
			return 0, &Failure{Code: CodeStaleRef, Ref: ref, Detail: fmt.Sprintf("%s is no longer on the page; take a new snapshot", ref)}
		}
		return 0, engineFailure(err)
	}
	return target.node, nil
}

func (t *tab) resolveObject(ctx context.Context, node int64) (string, error) {
	var r struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := t.call(ctx, "DOM.resolveNode", map[string]any{"backendNodeId": node, "objectGroup": objectGroup}, &r); err != nil {
		if isProtocolError(err) {
			return "", fail(CodeStaleRef, "the element is no longer on the page")
		}
		return "", engineFailure(err)
	}
	return r.Object.ObjectID, nil
}

// describe names a DOM element by its tag and identifying attributes.
func (t *tab) describe(ctx context.Context, node int64) string {
	var d struct {
		Node struct {
			LocalName  string   `json:"localName"`
			Attributes []string `json:"attributes"`
		} `json:"node"`
	}
	if t.call(ctx, "DOM.describeNode", map[string]any{"backendNodeId": node}, &d) != nil {
		return "another element"
	}
	b := strings.Builder{}
	b.WriteString("<" + d.Node.LocalName)
	for i := 0; i+1 < len(d.Node.Attributes); i += 2 {
		switch d.Node.Attributes[i] {
		case "id", "class", "role", "aria-label":
			fmt.Fprintf(&b, " %s=%q", d.Node.Attributes[i], clip(d.Node.Attributes[i+1], 60))
		}
	}
	b.WriteString(">")
	return b.String()
}

func (t *tab) mouse(ctx context.Context, kind string, x, y float64, clicks int) error {
	before := t.navigationCount()
	params := map[string]any{"type": kind, "x": x, "y": y}
	if clicks > 0 {
		params["button"], params["clickCount"] = "left", clicks
	}
	if err := t.call(ctx, "Input.dispatchMouseEvent", params, nil); err != nil {
		return engineFailure(err)
	}
	t.mu.Lock()
	if t.navigated == before {
		t.pointer = pointerState{x: x, y: y, visible: true}
	}
	t.mu.Unlock()
	return nil
}

// drag moves the pointer with the left button held.
func (t *tab) drag(ctx context.Context, x, y float64) error {
	return engineFailure(t.call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseMoved", "x": x, "y": y, "button": "left", "buttons": 1,
	}, nil))
}

func (t *tab) click(ctx context.Context, x, y float64, clicks int) error {
	before := t.navigationCount()
	defer func() {
		t.mu.Lock()
		if t.navigated != before {
			t.pointer = pointerState{}
		}
		t.mu.Unlock()
	}()
	if err := t.mouse(ctx, "mouseMoved", x, y, 0); err != nil {
		return err
	}
	for c := 1; c <= clicks; c++ {
		if err := t.mouse(ctx, "mousePressed", x, y, c); err != nil {
			return err
		}
		if err := t.mouse(ctx, "mouseReleased", x, y, c); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) focus(ctx context.Context, t *tab, ref string, selectAll bool) error {
	node, err := s.node(ctx, t, ref)
	if err != nil {
		return err
	}
	if err := t.call(ctx, "DOM.focus", map[string]any{"backendNodeId": node}, nil); err != nil {
		if isProtocolError(err) {
			return &Failure{Code: CodeNotVisible, Ref: ref, Detail: fmt.Sprintf("%s cannot take focus", ref)}
		}
		return engineFailure(err)
	}
	if !selectAll {
		return nil
	}
	obj, err := t.resolveObject(ctx, node)
	if err != nil {
		return err
	}
	return engineFailure(t.call(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId": obj,
		"functionDeclaration": `function(){
			if (typeof this.select === "function") { this.select(); return; }
			if (this.isContentEditable) {
				const r = document.createRange(); r.selectNodeContents(this);
				const s = getSelection(); s.removeAllRanges(); s.addRange(r);
			}
		}`,
	}, nil))
}

func (t *tab) press(ctx context.Context, chord string) error {
	def, modifiers, ok := parseKey(chord)
	if !ok {
		return fail(CodeBadStep, "%q is not a key this can press; use a name like Enter, Tab, ArrowDown, or a single character, with modifiers joined by +", chord)
	}
	down := map[string]any{
		"type": "keyDown", "key": def.key, "code": def.code, "modifiers": modifiers,
		"windowsVirtualKeyCode": def.vk, "nativeVirtualKeyCode": def.vk,
	}
	if def.text != "" {
		down["text"], down["unmodifiedText"] = def.text, def.text
	} else {
		down["type"] = "rawKeyDown"
	}
	if err := t.call(ctx, "Input.dispatchKeyEvent", down, nil); err != nil {
		return engineFailure(err)
	}
	return engineFailure(t.call(ctx, "Input.dispatchKeyEvent", map[string]any{
		"type": "keyUp", "key": def.key, "code": def.code, "modifiers": modifiers,
		"windowsVirtualKeyCode": def.vk, "nativeVirtualKeyCode": def.vk,
	}, nil))
}

func (s *Session) selectOptions(ctx context.Context, t *tab, step Step) (string, error) {
	node, err := s.node(ctx, t, step.Ref)
	if err != nil {
		return "", err
	}
	obj, err := t.resolveObject(ctx, node)
	if err != nil {
		return "", err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := t.call(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId": obj,
		"functionDeclaration": `function(values){
			if (!(this instanceof HTMLSelectElement)) return -1;
			const want = new Set(values); let hit = 0;
			for (const o of this.options) {
				const on = want.has(o.value) || want.has(o.label);
				if (on || !this.multiple) o.selected = on;
				if (on) hit++;
			}
			if (hit) { this.dispatchEvent(new Event("input", {bubbles: true})); this.dispatchEvent(new Event("change", {bubbles: true})); }
			return hit;
		}`,
		"arguments":     []map[string]any{{"value": step.Values}},
		"returnByValue": true,
	}, &r); err != nil {
		return "", engineFailure(err)
	}
	var hit int
	_ = json.Unmarshal(r.Result.Value, &hit)
	switch {
	case hit < 0:
		return "", &Failure{Code: CodeNotSelectable, Ref: step.Ref, Detail: fmt.Sprintf("%s is not a <select>; click it and choose from what opens instead", step.Ref)}
	case hit == 0:
		return "", &Failure{Code: CodeNoSuchOption, Ref: step.Ref, Detail: fmt.Sprintf("%s has no option with value or label %q", step.Ref, step.Values)}
	}
	return fmt.Sprintf("select %d option(s) in %s", hit, step.Ref), nil
}

func (s *Session) scrollPoint(ctx context.Context, t *tab, step Step) (float64, float64, error) {
	if step.Ref != "" || (step.X != nil && step.Y != nil) {
		return s.point(ctx, t, step)
	}
	vp, err := t.viewport(ctx)
	if err != nil {
		return 0, 0, err
	}
	return vp.Width / 2, vp.Height / 2, nil
}

func (t *tab) waitForText(ctx context.Context, step Step) (string, error) {
	if step.Text == "" {
		return "", fail(CodeBadStep, "a wait_for step needs text to wait for")
	}
	limit := defaultWaitFor
	if step.Ms > 0 {
		limit = min(time.Duration(step.Ms)*time.Millisecond, maxWait)
	}
	quoted, _ := json.Marshal(step.Text)
	expr := fmt.Sprintf("!!document.body && document.body.innerText.includes(%s)", quoted)
	deadline := time.Now().Add(limit)
	for {
		var r struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		if err := t.call(ctx, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true}, &r); err != nil && !isProtocolError(err) {
			return "", engineFailure(err)
		}
		if r.Result.Value {
			return fmt.Sprintf("found %q", step.Text), nil
		}
		if time.Now().After(deadline) {
			return "", fail(CodeWaitTimeout, "%q did not appear within %s", step.Text, limit)
		}
		select {
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func (t *tab) answerDialog(ctx context.Context, step Step) (string, error) {
	d := t.currentDialog()
	if d == nil {
		return "", fail(CodeNoDialog, "no dialog is open")
	}
	accept := step.Accept == nil || *step.Accept
	params := map[string]any{"accept": accept}
	if step.Text != "" {
		params["promptText"] = step.Text
	}
	if err := t.call(ctx, "Page.handleJavaScriptDialog", params, nil); err != nil {
		return "", engineFailure(err)
	}
	verb := "dismissed"
	if accept {
		verb = "accepted"
	}
	return fmt.Sprintf("%s the %s dialog", verb, d.Type), nil
}

// history moves one entry back (-1) or forward (+1) in this tab's own history.
func (t *tab) history(ctx context.Context, step int) (string, error) {
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := t.call(ctx, "Page.getNavigationHistory", nil, &h); err != nil {
		return "", engineFailure(err)
	}
	want := h.CurrentIndex + step
	if want < 0 || want >= len(h.Entries) {
		side := "earlier"
		if step > 0 {
			side = "later"
		}
		return "", fail(CodeNavigationFailed, "there is no %s page in this tab", side)
	}
	before := t.navigationCount()
	if err := t.call(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": h.Entries[want].ID}, nil); err != nil {
		return "", engineFailure(err)
	}
	t.settle(ctx, before)
	url, _ := t.location()
	verb := "back to "
	if step > 0 {
		verb = "forward to "
	}
	return verb + url, nil
}

// reload loads the page again from the server, which is what a change to the
// file it was served from needs; a second open of the same address may answer
// from the cache.
func (t *tab) reload(ctx context.Context) (string, error) {
	before := t.navigationCount()
	if err := t.call(ctx, "Page.reload", map[string]any{"ignoreCache": true}, nil); err != nil {
		return "", engineFailure(err)
	}
	t.settle(ctx, before)
	if err := t.waitAnyLoad(ctx, navigationTimeout); err != nil {
		return "", err
	}
	url, _ := t.location()
	return "reload " + url, nil
}

// drag presses at one point and releases at another, moving in steps so a page
// that follows the pointer sees the move rather than a jump.
func (s *Session) drag(ctx context.Context, t *tab, step Step) (string, error) {
	fromX, fromY, err := s.point(ctx, t, step)
	if err != nil {
		return "", err
	}
	to := Step{Action: step.Action, Ref: step.ToRef, X: step.ToX, Y: step.ToY}
	if to.Ref == "" && (to.X == nil || to.Y == nil) {
		return "", fail(CodeBadStep, "a drag needs where it ends: to_ref, or to_x and to_y")
	}
	toX, toY, err := s.point(ctx, t, to)
	if err != nil {
		return "", err
	}
	if err := t.mouse(ctx, "mouseMoved", fromX, fromY, 0); err != nil {
		return "", err
	}
	if err := t.mouse(ctx, "mousePressed", fromX, fromY, 1); err != nil {
		return "", err
	}
	for i := 1; i <= dragSteps; i++ {
		at := float64(i) / dragSteps
		if err := t.drag(ctx, fromX+(toX-fromX)*at, fromY+(toY-fromY)*at); err != nil {
			return "", err
		}
	}
	if err := t.mouse(ctx, "mouseReleased", toX, toY, 1); err != nil {
		return "", err
	}
	t.settle(ctx, t.navigationCount())
	return fmt.Sprintf("drag %s to %s", target(step), target(to)), nil
}

// upload hands a file input the files it would have been given by a person.
// Only a file inside the workspace: the page decides what to do with what it
// is given, and the agent may not hand it anything else.
func (s *Session) upload(ctx context.Context, t *tab, step Step) (string, error) {
	if len(step.Files) == 0 {
		return "", fail(CodeBadStep, "an upload needs files")
	}
	node, err := s.node(ctx, t, step.Ref)
	if err != nil {
		return "", err
	}
	paths, err := uploadPaths(step.Files, s.cfg.Roots)
	if err != nil {
		return "", err
	}
	if err := t.call(ctx, "DOM.setFileInputFiles", map[string]any{"backendNodeId": node, "files": paths}, nil); err != nil {
		if isProtocolError(err) {
			return "", &Failure{Code: CodeBadStep, Ref: step.Ref, Detail: fmt.Sprintf("%s does not take files; upload needs the page's file input", step.Ref)}
		}
		return "", engineFailure(err)
	}
	return fmt.Sprintf("give %s %d file(s)", step.Ref, len(paths)), nil
}

// uploadPaths vets the files an upload names: each must be a file on this
// machine inside roots, and a network path is refused before it is looked up.
func uploadPaths(files, roots []string) ([]string, error) {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if spelledAsNetwork(file) && judgeFile(file, roots) != fileInside {
			return nil, networkRefusal(file, "a page may only be given a file inside the workspace")
		}
		abs, err := filepath.Abs(file)
		if err != nil {
			return nil, fail(CodeURLRefused, "%q is outside the workspace; a page may only be given a file inside it", file)
		}
		switch judgeFile(abs, roots) {
		case fileNetwork:
			return nil, networkRefusal(file, "a page may only be given a file inside the workspace")
		case fileOutside:
			return nil, fail(CodeURLRefused, "%q is outside the workspace; a page may only be given a file inside it", file)
		}
		if _, err := statFile(abs); err != nil {
			return nil, fail(CodeBadStep, "%q is not a file on this machine", file)
		}
		paths = append(paths, abs)
	}
	return paths, nil
}

// The viewport a page may be asked to lay out in. The floor is the smallest
// phone worth testing; the ceiling keeps a screenshot within what a vision
// model is given.
const (
	minViewport = 200
	maxViewport = 4000
)

// resize lays the page out in a viewport of its own, whatever size the window
// is. It is how a page is seen at a phone's width without one.
func (t *tab) resize(ctx context.Context, step Step) (string, error) {
	if step.Width < minViewport || step.Height < minViewport || step.Width > maxViewport || step.Height > maxViewport {
		return "", fail(CodeBadStep, "a resize needs width and height in CSS pixels, between %d and %d", minViewport, maxViewport)
	}
	args := map[string]any{"width": step.Width, "height": step.Height, "deviceScaleFactor": 0, "mobile": false}
	if err := t.call(ctx, "Emulation.setDeviceMetricsOverride", args, nil); err != nil {
		return "", engineFailure(err)
	}
	t.mu.Lock()
	t.pointer = pointerState{}
	t.shotScale = 0
	t.mu.Unlock()
	return fmt.Sprintf("resize to %d×%d", step.Width, step.Height), nil
}
