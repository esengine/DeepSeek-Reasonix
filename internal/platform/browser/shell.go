package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// shellTimeout bounds every HTTP call to the shell's WebView. A page that
// hangs the WebView's JavaScript thread would otherwise hold the tool call
// open indefinitely; the timeout turns that into a failure the model can
// retry or report.
const shellTimeout = 30 * time.Second

// maxReadText is how much page text the shell is asked to return. A page
// that renders megabytes of text would blow the model's context; the shell
// truncates at this limit and says so.
const maxReadText = 400 << 10 // 400 KB

// ShellSession is a BrowserSession backed by the phone's own WebView, reached
// over loopback HTTP. The Kotlin shell runs a small HTTP server beside the
// kernel; this session is the kernel's half of that conversation.
//
// The WebView is a single top-level browsing context. "Tabs" are a fiction
// maintained here so the tools' tab bookkeeping works unchanged: one real
// WebView, addressed as "t1". Open with newTab navigates the same view; a
// second "tab" is not created. This is the honest answer a phone can give —
// one screen, one page at a time — and the tools read it that way because
// Tabs() always returns one entry.
//
// The snapshot is a text extraction rather than an accessibility tree: the
// WebView has no CDP endpoint, so the shell injects a script that walks the
// DOM and returns a structured text rendering. It is coarser than a Chromium
// AX tree. Interactive elements do carry refs: the shell writes a
// `data-rx-ref` attribute on each one and renders it as `[eN]`, and an act
// step naming a ref resolves that same attribute through a CSS selector. A ref
// dies with the document it was issued on — the attribute goes with the DOM —
// and a step that uses one are answered with a failure that says to take a new
// snapshot. The model learns the shape of this backend from the first
// snapshot it reads.
type ShellSession struct {
	base   string // "http://127.0.0.1:<port>"
	token  string // the bearer credential the shell minted, "" when it asks for none
	client *http.Client
	roots  []string

	mu       sync.Mutex
	changed  func()
	owners   int
	closed   bool
	url      string // the WebView's current URL, as the shell last reported it
	title    string
	secrets  map[string]bool   // origin → approved for credential entry
	scripts  map[string]string // args hash → origin the script was approved for
	tabState string            // "open" or "" — the single tab exists or does not
	els      []shellElement    // interactive elements of the document the last snapshot read
}

// NewShellSession creates a session that drives the shell's WebView.
// baseURL is the shell's browser HTTP server, e.g. "http://127.0.0.1:12345".
// roots bound which local files a page may be, matching Session's Roots.
// token is the shared secret the shell minted for this launch; every call
// carries it as a bearer credential.
func NewShellSession(baseURL string, roots []string, token string) *ShellSession {
	return &ShellSession{
		base:    strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: shellTimeout},
		roots:   roots,
		secrets: map[string]bool{},
		scripts: map[string]string{},
	}
}

// shellCall is one POST to the shell's browser server.
func (s *ShellSession) shellCall(ctx context.Context, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fail(CodeEngineFailed, "encode request: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+path, reader)
	if err != nil {
		return fail(CodeEngineFailed, "build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The credential the shell minted. Without it the shell refuses every
	// route: the server listens on loopback, which any other process on the
	// phone can reach, and the routes it exposes are `eval`, `read` and
	// `screenshot` on whatever page is loaded. A bearer token is the least
	// that makes "only the kernel this shell started" true.
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fail(CodeEngineFailed, "the shell's browser view is not reachable: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fail(CodeEngineFailed, "read the shell's answer: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		var shellErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &shellErr) == nil && shellErr.Error != "" {
			return fail(CodeEngineFailed, "%s", shellErr.Error)
		}
		return fail(CodeEngineFailed, "the shell answered %d: %s", resp.StatusCode, string(data))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fail(CodeEngineFailed, "the shell's answer was not valid JSON: %v", err)
		}
	}
	return nil
}

// alive checks whether the shell's browser server is answering.
func (s *ShellSession) alive(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/health", nil)
	if err != nil {
		return false
	}
	// The shell refuses every route without the credential, `/health` included,
	// so a probe that omitted it would read "not answering" on a server that is.
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// --- Navigation ---

func (s *ShellSession) Open(ctx context.Context, rawURL, tabID string, newTab bool) (TabInfo, error) {
	target, err := checkURL(rawURL, s.roots)
	if err != nil {
		return TabInfo{}, err
	}
	return s.load(ctx, target)
}

func (s *ShellSession) Visit(ctx context.Context, rawURL, tabID string, newTab bool) (TabInfo, error) {
	return s.Open(ctx, rawURL, tabID, newTab)
}

func (s *ShellSession) VisitBeside(ctx context.Context, rawURL string) (TabInfo, error) {
	return s.Open(ctx, rawURL, "", true)
}

func (s *ShellSession) load(ctx context.Context, url string) (TabInfo, error) {
	var resp struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	if err := s.shellCall(ctx, "/load", map[string]string{"url": url}, &resp); err != nil {
		return TabInfo{}, err
	}
	s.mu.Lock()
	s.url = resp.URL
	s.title = resp.Title
	s.tabState = "open"
	s.mu.Unlock()
	// This is a different document from the one the last snapshot read, so the
	// refs it listed no longer name anything here.
	s.forgetRefs()
	s.tabsChanged()
	return s.tabInfo(), nil
}

func (s *ShellSession) Switch(tabID string) (TabInfo, error) {
	if tabID != "" && tabID != "t1" {
		return TabInfo{}, fail(CodeNoTab, "no tab %s in this session", tabID)
	}
	s.mu.Lock()
	state := s.tabState
	s.mu.Unlock()
	if state != "open" {
		return TabInfo{}, fail(CodeNoTab, "no page is open; open one first")
	}
	return s.tabInfo(), nil
}

func (s *ShellSession) CloseTab(ctx context.Context, tabID string) error {
	if err := s.shellCall(ctx, "/close", map[string]string{}, nil); err != nil {
		return err
	}
	s.mu.Lock()
	s.tabState = ""
	s.url = ""
	s.title = ""
	s.mu.Unlock()
	s.forgetRefs()
	s.tabsChanged()
	return nil
}

func (s *ShellSession) Tabs() []TabInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tabState != "open" {
		return nil
	}
	return []TabInfo{{ID: "t1", Target: "t1", URL: s.url, Title: s.title, Active: true}}
}

// --- Reading ---

func (s *ShellSession) Snapshot(ctx context.Context, tabID, scope string) (Snapshot, error) {
	var resp struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Text  string `json:"text"`
		// The shell sets this when the page in the view is its own error page
		// rather than a site. Reading that page back as content would quote the
		// browser's "this page could not be loaded" to the model as though the
		// site had said it. The shell clears the state when a new navigation
		// starts, so this describes the document the view is on now rather than
		// the last one that failed.
		Reason string `json:"reason"`
		// The interactive elements of the page, each with the ref the shell
		// stamped on it, in document order. The shell has no accessibility
		// tree, so this is the list it can honestly give; the refs in it are
		// what an act step names.
		Elements []shellElement `json:"elements"`
	}
	if err := s.shellCall(ctx, "/read", map[string]string{}, &resp); err != nil {
		return Snapshot{}, err
	}
	if resp.Reason != "" {
		return Snapshot{}, fail(CodeNavigationFailed, "%s: %s", resp.URL, resp.Reason)
	}
	s.mu.Lock()
	s.els = resp.Elements
	wasURL, wasTitle := s.url, s.title
	s.url = resp.URL
	s.title = resp.Title
	s.mu.Unlock()
	// The address and title shown beside the page come from here, so a tab that
	// moved while the agent was reading it has to say so. Without this the
	// panel keeps naming where the page used to be — a failed load's address,
	// long after the page after it arrived.
	if resp.URL != wasURL || resp.Title != wasTitle {
		s.tabsChanged()
	}

	lines := buildTextSnapshot(resp.Text)
	if refs := renderShellRefs(resp.Elements); len(refs) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Elements you can act on:")
		lines = append(lines, refs...)
	}
	return Snapshot{
		Tab:   s.tabInfo(),
		Lines: lines,
	}, nil
}

// shellElement is one interactive element the shell found on a page: the ref
// it stamped on the DOM node, what the element is, and the words it carries.
type shellElement struct {
	Ref   string `json:"ref"`
	Role  string `json:"role"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// renderShellRefs is how the shell's element list is shown to the model, in
// the same shape the Chromium backend uses: `- role "name" [eN]`.
func renderShellRefs(els []shellElement) []string {
	out := make([]string, 0, len(els))
	for _, e := range els {
		if e.Ref == "" {
			continue
		}
		line := "- " + e.Role
		if e.Name != "" {
			line += " " + strconv.Quote(clip(e.Name, maxNameRunes))
		}
		line += " [" + e.Ref + "]"
		if e.Value != "" && e.Value != e.Name {
			line += " value=" + strconv.Quote(clip(e.Value, maxNameRunes))
		}
		out = append(out, line)
	}
	return out
}

func (s *ShellSession) Screenshot(ctx context.Context, tabID string) (string, TabInfo, error) {
	var resp struct {
		Data string `json:"data"` // base64
		Mime string `json:"mime"`
	}
	if err := s.shellCall(ctx, "/screenshot", map[string]string{}, &resp); err != nil {
		return "", TabInfo{}, err
	}
	return "data:" + resp.Mime + ";base64," + resp.Data, s.tabInfo(), nil
}

func (s *ShellSession) Logs(tabID string) ([]LogEntry, TabInfo, error) {
	// The shell does not collect console messages.
	return nil, s.tabInfo(), nil
}

func (s *ShellSession) Requests(tabID string) ([]Request, TabInfo, error) {
	// The phone's WebView reports what a page asked for, from its request
	// interceptor. It does not expose what came back — no status, no size — so
	// each entry is marked as a request whose response this backend cannot see,
	// rather than left looking like one still in flight.
	var resp struct {
		Requests []struct {
			Method string `json:"method"`
			URL    string `json:"url"`
			Kind   string `json:"kind"`
		} `json:"requests"`
	}
	if err := s.shellCall(context.Background(), "/requests", map[string]string{}, &resp); err != nil {
		return nil, s.tabInfo(), err
	}
	out := make([]Request, 0, len(resp.Requests))
	for _, r := range resp.Requests {
		out = append(out, Request{Method: r.Method, URL: r.URL, Kind: r.Kind, ResponseUnknown: true})
	}
	return out, s.tabInfo(), nil
}

func (s *ShellSession) Refresh(ctx context.Context, tabID string) (Snapshot, Changes, error) {
	snap, err := s.Snapshot(ctx, tabID, "")
	if err != nil {
		return Snapshot{}, Changes{}, err
	}
	// A WebView has no accessibility tree diff; the whole page is "new" each
	// time. The tools render that as a full snapshot rather than a diff.
	return snap, Changes{NewDocument: true}, nil
}

// --- Acting ---

func (s *ShellSession) Act(ctx context.Context, tabID string, steps []Step, secrets bool, scriptOrigin string) (ActResult, error) {
	res := ActResult{FailedAt: -1, Tab: s.tabInfo()}
	for i, step := range steps {
		note, err := s.runStep(ctx, step, secrets, scriptOrigin)
		if err != nil {
			res.FailedAt = i
			return res, err
		}
		res.Done++
		res.Notes = append(res.Notes, note)
	}
	return res, nil
}

func (s *ShellSession) runStep(ctx context.Context, step Step, secrets bool, scriptOrigin string) (string, error) {
	action := strings.ToLower(strings.TrimSpace(step.Action))
	switch action {
	case "click":
		// Click by ref (the attribute the shell stamped) or by the step's own
		// CSS selector: either way the shell injects a script.
		sel, err := s.selectorFor(step)
		if err != nil {
			return "", err
		}
		var resp struct {
			OK bool `json:"ok"`
		}
		if err := s.shellCall(ctx, "/click", map[string]string{"selector": sel}, &resp); err != nil {
			return "", err
		}
		// The injected script answers whether it found anything to click. A
		// page that moved between the snapshot and this step leaves the ref
		// pointing at nothing, and reporting that as a click would tell the
		// model it acted on a page it did not touch.
		if !resp.OK {
			return "", &Failure{Code: CodeStaleRef, Ref: step.Ref, Detail: fmt.Sprintf("nothing on the page matches %s; take a new snapshot and use one of its refs", sel)}
		}
		return "clicked " + sel, nil

	case "eval":
		script := strings.TrimSpace(step.Script)
		if script == "" {
			return "", fail(CodeBadStep, "eval needs a script")
		}
		if scriptOrigin == "" {
			return "", fail(CodeOriginChanged, "the script was not approved for this page")
		}
		var resp struct {
			Result string `json:"result"`
		}
		if err := s.shellCall(ctx, "/eval", map[string]string{"script": script}, &resp); err != nil {
			return "", err
		}
		result := resp.Result
		if len(result) > maxEvalResult {
			result = result[:maxEvalResult] + "\n… result truncated"
		}
		return result, nil

	case "fill", "type":
		sel, err := s.selectorFor(step)
		if err != nil {
			return "", err
		}
		var resp struct {
			Result string `json:"result"`
		}
		// An element that is no longer there returns the empty string, which is
		// how this reports a ref the page has moved past rather than silently
		// typing into whatever field happened to be first.
		script := fmt.Sprintf(`(function(){var el=document.querySelector(%q);if(!el)return '';el.focus();el.value=%q;el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));return 'filled '+el.value.length+' characters'})()`, sel, step.Text)
		if err := s.shellCall(ctx, "/eval", map[string]string{"script": script}, &resp); err != nil {
			return "", err
		}
		if strings.TrimSpace(resp.Result) == "" {
			return "", &Failure{Code: CodeStaleRef, Ref: step.Ref, Detail: fmt.Sprintf("nothing on the page matches %s; take a new snapshot and use one of its refs", sel)}
		}
		return resp.Result, nil

	case "select":
		sel, err := s.selectorFor(step)
		if err != nil {
			return "", err
		}
		if len(step.Values) == 0 {
			return "", fail(CodeBadStep, "select needs values: the option values or labels to choose")
		}
		want, err := json.Marshal(step.Values)
		if err != nil {
			return "", fail(CodeBadStep, "select values could not be read: %v", err)
		}
		var resp struct {
			Result string `json:"result"`
		}
		// An option is matched by value or by the words it shows, because a
		// caller reads the label off the snapshot and does not see the value.
		script := fmt.Sprintf(`(function(){var el=document.querySelector(%q);if(!el)return '';if(el.tagName.toLowerCase()!=='select')return 'not a select';var want=%s;var opts=el.options;var chosen=0;for(var i=0;i<opts.length;i++){if(want.indexOf(opts[i].value)>=0||want.indexOf((opts[i].text||'').trim())>=0){opts[i].selected=true;chosen++;}}if(!chosen)return 'nothing matches '+want.join(', ');el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));return 'selected '+chosen+' option(s)'})()`, sel, want)
		if err := s.shellCall(ctx, "/eval", map[string]string{"script": script}, &resp); err != nil {
			return "", err
		}
		if strings.TrimSpace(resp.Result) == "" {
			return "", &Failure{Code: CodeStaleRef, Ref: step.Ref, Detail: fmt.Sprintf("nothing on the page matches %s; take a new snapshot and use one of its refs", sel)}
		}
		return resp.Result, nil

	case "scroll":
		var resp struct {
			Result string `json:"result"`
		}
		// The page scrolls itself: a WebView has no input channel to send a
		// wheel through, and scrolling the page is what a caller is asking for
		// either way. Scrolling inside a scrollable element is not reachable
		// this way and is what eval is for.
		script := fmt.Sprintf(`(function(){window.scrollBy(%v,%v);return 'scrolled '+window.scrollX+','+window.scrollY})()`, step.DeltaX, step.DeltaY)
		if err := s.shellCall(ctx, "/eval", map[string]string{"script": script}, &resp); err != nil {
			return "", err
		}
		if step.DeltaX != 0 && step.DeltaY == 0 {
			return fmt.Sprintf("scroll %v px sideways", step.DeltaX), nil
		}
		return fmt.Sprintf("scroll %v px", step.DeltaY), nil

	case "back":
		if err := s.shellCall(ctx, "/back", nil, nil); err != nil {
			return "", err
		}
		s.forgetRefs()
		return "went back", nil

	case "forward":
		if err := s.shellCall(ctx, "/forward", nil, nil); err != nil {
			return "", err
		}
		s.forgetRefs()
		return "went forward", nil

	case "reload":
		if err := s.shellCall(ctx, "/reload", nil, nil); err != nil {
			return "", err
		}
		s.forgetRefs()
		return "reloaded", nil

	case "wait":
		ms := step.Ms
		if ms <= 0 {
			ms = 1000
		}
		if ms > int(maxWait.Milliseconds()) {
			ms = int(maxWait.Milliseconds())
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return fmt.Sprintf("waited %dms", ms), nil

	case "hover", "double_click", "drag", "upload", "press", "dialog", "resize", "wait_for":
		return "", fail(CodeBadStep, "the %s action needs input this WebView cannot send; use click, fill, select or scroll with a ref from the snapshot, or eval", action)
	}
	return "", fail(CodeBadStep, "%q is not a known action", action)
}

// forgetRefs drops the refs the last snapshot read.
//
// A ref names an element of the document that snapshot was taken from. Once
// this view has moved to another document — a navigation, a reload, a step
// back or forward — that numbering no longer applies, and a ref kept here would
// be accepted for whatever element of the new page answers the same attribute.
// Dropping them makes every ref from before the move fail as stale, which is
// what it is; the caller takes a new snapshot and uses the refs it lists.
func (s *ShellSession) forgetRefs() {
	s.mu.Lock()
	s.els = nil
	s.mu.Unlock()
}

// selectorFor is the CSS selector a step runs against. A ref names an element
// the shell stamped during the last snapshot, and is checked against that
// snapshot's list so a ref from a page since navigated away from is refused
// rather than resolving to whatever now answers the same attribute. A click
// with no ref keeps the older path of the step's own text as a selector; every
// other action has to name a ref, because those are the only targets this
// WebView can identify.
func (s *ShellSession) selectorFor(step Step) (string, error) {
	ref := strings.TrimSpace(step.Ref)
	if ref == "" {
		if a := strings.ToLower(strings.TrimSpace(step.Action)); a == "click" {
			if sel := strings.TrimSpace(step.Text); sel != "" {
				return sel, nil
			}
			return "", fail(CodeBadStep, "click needs a ref from the snapshot, or a CSS selector in text")
		}
		return "", fail(CodeBadStep, "the %s action needs a ref: take a snapshot and use one of the [eN] refs it lists", step.Action)
	}
	s.mu.Lock()
	known := false
	for _, e := range s.els {
		if e.Ref == ref {
			known = true
			break
		}
	}
	s.mu.Unlock()
	if !known {
		return "", &Failure{Code: CodeStaleRef, Ref: ref, Detail: fmt.Sprintf("%s is not on the page the last snapshot read; take a new snapshot and use one of its refs", ref)}
	}
	return `[data-rx-ref="` + ref + `"]`, nil
}

// --- Identity ---

func (s *ShellSession) Origin(tabID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return OriginOf(s.url)
}

func (s *ShellSession) PageURL(tabID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.url
}

func (s *ShellSession) ServesWorkspace(url string) bool {
	// The shell's WebView loads pages from the network; it does not serve
	// workspace files. A page loaded from a file:// inside the workspace is
	// the one exception.
	s.mu.Lock()
	u := s.url
	s.mu.Unlock()
	return strings.HasPrefix(u, "file://") && judgeFile(strings.TrimPrefix(u, "file://"), s.roots) == fileInside
}

// --- Credential and script confirmation ---

func (s *ShellSession) CredentialEntry(ctx context.Context, tabID string, steps []Step) bool {
	// The shell has no way to identify password fields by their autocomplete
	// attribute, so it does not gate credential entry. The host's approval
	// still names the origin through PermissionArgs.
	return false
}

func (s *ShellSession) NoteSecretCheck(args []byte, secret bool) {
	if !secret {
		return
	}
	origin := s.Origin("")
	s.mu.Lock()
	s.secrets[origin] = true
	s.mu.Unlock()
}

func (s *ShellSession) NoteScriptOrigin(args []byte, origin string) {
	key := string(args)
	s.mu.Lock()
	s.scripts[key] = origin
	s.mu.Unlock()
}

func (s *ShellSession) SecretsConfirmed(args []byte) bool {
	origin := s.Origin("")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets[origin]
}

func (s *ShellSession) ScriptOrigin(args []byte) string {
	key := string(args)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scripts[key]
}

// --- Lifecycle ---

func (s *ShellSession) OnTabsChanged(fn func()) {
	s.mu.Lock()
	s.changed = fn
	s.mu.Unlock()
}

func (s *ShellSession) tabsChanged() {
	s.mu.Lock()
	fn := s.changed
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *ShellSession) Retain() {
	s.mu.Lock()
	s.owners++
	s.mu.Unlock()
}

func (s *ShellSession) Release() {
	s.mu.Lock()
	if s.owners > 0 {
		s.owners--
	}
	last := s.owners == 0
	s.mu.Unlock()
	if last {
		s.Close()
	}
}

func (s *ShellSession) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// tabInfo is the single tab's current state.
func (s *ShellSession) tabInfo() TabInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	active := s.tabState == "open"
	return TabInfo{ID: "t1", Target: "t1", URL: s.url, Title: s.title, Active: active}
}

// buildTextSnapshot turns extracted page text into the line-per-element shape
// the tools render. Without an accessibility tree the lines are the text's
// own paragraphs, headings and list items, split on newlines.
func buildTextSnapshot(text string) []string {
	if strings.TrimSpace(text) == "" {
		return []string{"The page shows nothing."}
	}
	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{"The page shows nothing."}
	}
	return out
}
