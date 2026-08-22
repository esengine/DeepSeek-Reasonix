package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/browserrelay"
	"reasonix/internal/tool"
)

// RelayBound marks browser_* tools that only function when a browser relay
// server is running (desktop runtime). boot skips them in CLI/ACP sessions so
// unusable schemas are never advertised to the model.
type RelayBound interface {
	relayBound()
}

// evalParams builds Runtime.evaluate params that request the result by value,
// so object/array results carry a usable `value` field instead of a remote
// object reference the Go side cannot decode.
func evalParams(expression string, extra ...map[string]interface{}) json.RawMessage {
	params := map[string]interface{}{"expression": expression, "returnByValue": true}
	for _, e := range extra {
		for k, v := range e {
			params[k] = v
		}
	}
	raw, _ := json.Marshal(params)
	return raw
}

// evaluateException extracts a JavaScript exception raised inside a successful
// Runtime.evaluate response. CDP reports JS errors in exceptionDetails, not as
// a command error, so callers must inspect it before reporting success.
func evaluateException(result json.RawMessage) error {
	var evalResult struct {
		Result struct {
			Type  string `json:"type"`
			Value any    `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		return nil // not a parseable evaluate response; caller handles it
	}
	if evalResult.ExceptionDetails != nil {
		msg := evalResult.ExceptionDetails.Text
		if evalResult.ExceptionDetails.Exception != nil && evalResult.ExceptionDetails.Exception.Description != "" {
			msg = evalResult.ExceptionDetails.Exception.Description
		}
		return fmt.Errorf("page script error: %s", msg)
	}
	return nil
}

func init() {
	tool.RegisterBuiltin(browserStatus{})
	tool.RegisterBuiltin(browserNavigate{})
	tool.RegisterBuiltin(browserClick{})
	tool.RegisterBuiltin(browserType{})
	tool.RegisterBuiltin(browserRead{})
	tool.RegisterBuiltin(browserScreenshot{})
	tool.RegisterBuiltin(browserEval{})

	// Tab management
	tool.RegisterBuiltin(browserListPages{})
	tool.RegisterBuiltin(browserSelectPage{})
	tool.RegisterBuiltin(browserNewPage{})
	tool.RegisterBuiltin(browserClosePage{})

	// Page interaction
	tool.RegisterBuiltin(browserReadDOM{})
	tool.RegisterBuiltin(browserScroll{})
	tool.RegisterBuiltin(browserGoBack{})
	tool.RegisterBuiltin(browserGoForward{})
	tool.RegisterBuiltin(browserPressKey{})
	tool.RegisterBuiltin(browserHover{})
	tool.RegisterBuiltin(browserWait{})
	tool.RegisterBuiltin(browserUploadFile{})
	tool.RegisterBuiltin(browserResize{})
	tool.RegisterBuiltin(browserHandleDialog{})
	tool.RegisterBuiltin(browserFillForm{})
	tool.RegisterBuiltin(browserAttachedPages{})
	tool.RegisterBuiltin(browserAttachPage{})
	tool.RegisterBuiltin(browserEmulate{})
	tool.RegisterBuiltin(browserTakeSnapshot{})
	tool.RegisterBuiltin(browserDrag{})
	tool.RegisterBuiltin(browserListConsoleMessages{})
	tool.RegisterBuiltin(browserListNetworkRequests{})
}

// relayBound marks every browser_* tool as relay-only (see RelayBound).
func (browserStatus) relayBound()              {}
func (browserNavigate) relayBound()            {}
func (browserClick) relayBound()               {}
func (browserType) relayBound()                {}
func (browserRead) relayBound()                {}
func (browserScreenshot) relayBound()          {}
func (browserEval) relayBound()                {}
func (browserListPages) relayBound()           {}
func (browserSelectPage) relayBound()          {}
func (browserNewPage) relayBound()             {}
func (browserClosePage) relayBound()           {}
func (browserReadDOM) relayBound()             {}
func (browserScroll) relayBound()              {}
func (browserGoBack) relayBound()              {}
func (browserGoForward) relayBound()           {}
func (browserPressKey) relayBound()            {}
func (browserHover) relayBound()               {}
func (browserWait) relayBound()                {}
func (browserUploadFile) relayBound()          {}
func (browserResize) relayBound()              {}
func (browserHandleDialog) relayBound()        {}
func (browserFillForm) relayBound()            {}
func (browserAttachedPages) relayBound()       {}
func (browserAttachPage) relayBound()          {}
func (browserEmulate) relayBound()             {}
func (browserTakeSnapshot) relayBound()        {}
func (browserDrag) relayBound()                {}
func (browserListConsoleMessages) relayBound() {}
func (browserListNetworkRequests) relayBound() {}

// FilterRelayTools drops browser_* tools when no browser relay server runs in
// this process. boot calls it for CLI/ACP registries so unusable schemas never
// reach the model; the desktop starts a relay before building sessions, so its
// registries keep the tools.
func FilterRelayTools(tools []tool.Tool) []tool.Tool {
	if browserrelay.Available() {
		return tools
	}
	out := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if _, ok := t.(RelayBound); ok {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ─── browser_status ────────────────────────────────────────────────────────

type browserStatus struct{}

func (browserStatus) Name() string   { return "browser_status" }
func (browserStatus) ReadOnly() bool { return true }
func (browserStatus) Description() string {
	return "Check if the browser relay extension is connected and authorized. Always call this first before using any other browser tool to verify the connection state. Returns the server address, connection state, and extension info."
}

func (browserStatus) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (browserStatus) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	s := browserrelay.GetStatus()
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b), nil
}

// ─── browser_navigate ──────────────────────────────────────────────────────

type browserNavigate struct{}

func (browserNavigate) Name() string   { return "browser_navigate" }
func (browserNavigate) ReadOnly() bool { return false }
func (browserNavigate) Description() string {
	return "Navigate the current browser tab to the specified URL. Waits for the page to finish loading before returning. After navigation, use browser_read or browser_read_dom to confirm the page loaded correctly. If the page doesn't load, check the URL format."
}

func (browserNavigate) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "url":{"type":"string","description":"The URL to navigate to"}
},
"required":["url"]
}`)
}

func (browserNavigate) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	raw, _ := json.Marshal(map[string]string{"url": params.URL})
	result, err := browserrelay.Send(ctx, "Page.navigate", raw)
	if err != nil {
		return "", fmt.Errorf("navigate failed: %w", err)
	}

	// Wait for the page to finish loading (guide §9.2): poll document.readyState
	// until "complete" or timeout.
	readyRaw := evalParams("document.readyState")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		stateResult, err := browserrelay.Send(ctx, "Runtime.evaluate", readyRaw)
		if err == nil {
			var parsed struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			if json.Unmarshal(stateResult, &parsed) == nil && parsed.Result.Value == "complete" {
				break
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_click ─────────────────────────────────────────────────────────

type browserClick struct{}

func (browserClick) Name() string   { return "browser_click" }
func (browserClick) ReadOnly() bool { return false }
func (browserClick) Description() string {
	return "Click an element in the current page by CSS selector. The element is scrolled into view before clicking. Use browser_read or browser_read_dom first to discover page content and identify the right selector. Prefer this over browser_eval for simple clicks."
}

func (browserClick) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "selector":{"type":"string","description":"CSS selector for the element to click"}
},
"required":["selector"]
}`)
}

func (browserClick) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Selector == "" {
		return "", fmt.Errorf("selector is required")
	}

	// First find the element, then click it.
	findJS := fmt.Sprintf(`document.querySelector(%q)`, params.Selector)
	findResult, err := browserrelay.Send(ctx, "Runtime.evaluate", evalParams(findJS))
	if err != nil {
		return "", fmt.Errorf("find element: %w", err)
	}
	_ = findResult // We rely on the extension to click anyway.

	// Use Input.dispatchMouseEvent for a real click.
	clickJS := fmt.Sprintf(`(()=>{
		const el = document.querySelector(%q);
		if (!el) throw new Error("element not found: "+%q);
		const rect = el.getBoundingClientRect();
		const x = rect.x + rect.width/2;
		const y = rect.y + rect.height/2;
		return {x: Math.round(x), y: Math.round(y)};
	})()`, params.Selector, params.Selector)

	coordResult, err := browserrelay.Send(ctx, "Runtime.evaluate", evalParams(clickJS))
	if err != nil {
		return "", fmt.Errorf("get coordinates: %w", err)
	}
	if err := evaluateException(coordResult); err != nil {
		return "", fmt.Errorf("click: %w", err)
	}

	// Parse coordinates from result.
	var evalResult struct {
		Result struct {
			Type  string `json:"type"`
			Value struct {
				X int `json:"x"`
				Y int `json:"y"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(coordResult, &evalResult); err != nil || evalResult.Result.Type != "object" {
		return "", fmt.Errorf("click: element found but could not extract coordinates (selector=%q, parse_err=%v)", params.Selector, err)
	}

	x, y := evalResult.Result.Value.X, evalResult.Result.Value.Y
	clickParams, _ := json.Marshal(map[string]interface{}{
		"type":       "mousePressed",
		"x":          x,
		"y":          y,
		"button":     "left",
		"clickCount": 1,
	})
	if _, err := browserrelay.Send(ctx, "Input.dispatchMouseEvent", clickParams); err != nil {
		return "", fmt.Errorf("click: %w", err)
	}
	releaseParams, _ := json.Marshal(map[string]interface{}{
		"type":       "mouseReleased",
		"x":          x,
		"y":          y,
		"button":     "left",
		"clickCount": 1,
	})
	if _, err := browserrelay.Send(ctx, "Input.dispatchMouseEvent", releaseParams); err != nil {
		return "", fmt.Errorf("release: %w", err)
	}

	return fmt.Sprintf(`{"clicked": true, "selector": %q, "x": %d, "y": %d}`, params.Selector, x, y), nil
}

// ─── browser_type ──────────────────────────────────────────────────────────

type browserType struct{}

func (browserType) Name() string   { return "browser_type" }
func (browserType) ReadOnly() bool { return false }
func (browserType) Description() string {
	return "Type text into an input field by CSS selector. Clears existing content before typing by default. Use browser_fill_form for a simpler alternative that also handles select dropdowns. Use browser_press_key for special keys like Enter or Tab after typing."
}

func (browserType) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "selector":{"type":"string","description":"CSS selector for the input element"},
  "text":{"type":"string","description":"Text to type"},
  "clear":{"type":"boolean","description":"Clear existing content first (default true)"}
},
"required":["selector","text"]
}`)
}

func (browserType) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Selector string `json:"selector"`
		Text     string `json:"text"`
		Clear    bool   `json:"clear"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Selector == "" || params.Text == "" {
		return "", fmt.Errorf("selector and text are required")
	}

	// Focus the element, optionally clear, then set value.
	clear := "true"
	if !params.Clear {
		clear = "false"
	}
	js := fmt.Sprintf(`(()=>{
		const el = document.querySelector(%q);
		if (!el) throw new Error("element not found: "+%q);
		el.focus();
		if (%s) el.value = '';
		el.value = %q;
		el.dispatchEvent(new Event('input', {bubbles:true}));
		el.dispatchEvent(new Event('change', {bubbles:true}));
		return true;
	})()`, params.Selector, params.Selector, clear, params.Text)

	raw := evalParams(js)
	result, err := browserrelay.Send(ctx, "Runtime.evaluate", raw)
	if err != nil {
		return "", fmt.Errorf("type text: %w", err)
	}
	if err := evaluateException(result); err != nil {
		return "", fmt.Errorf("type text: %w", err)
	}
	return fmt.Sprintf(`{"typed": true, "selector": %q, "text": %q, "clear": %v}`, params.Selector, params.Text, params.Clear), nil
}

// MaxBodyLength is the maximum number of characters returned by browser_read.
// Pages exceeding this limit are truncated with a note appended.
const MaxBodyLength = 100_000

// ─── browser_read ──────────────────────────────────────────────────────────

type browserRead struct{}

func (browserRead) Name() string   { return "browser_read" }
func (browserRead) ReadOnly() bool { return true }
func (browserRead) Description() string {
	return "Read the visible text content of the current page. Returns the page title, URL, and body text. This is the PREFERRED tool for reading page content. Use a CSS selector to read only a specific element. For interactive elements (buttons, links, inputs), use browser_read_dom instead."
}

func (browserRead) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "selector":{"type":"string","description":"Optional CSS selector to read a specific element instead of the whole page"}
},
"required":[]
}`)
}

func (browserRead) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var js string
	if params.Selector != "" {
		js = fmt.Sprintf(`(()=>{
			const el = document.querySelector(%q);
			if (!el) return JSON.stringify({error: "element not found: "+%q});
			return JSON.stringify({
				title: document.title,
				text: el.innerText || el.textContent || ""
			});
		})()`, params.Selector, params.Selector)
	} else {
		js = `JSON.stringify({
			title: document.title,
			text: document.body.innerText || document.body.textContent || "",
			url: location.href
		})`
	}

	raw := evalParams(js)
	result, err := browserrelay.Send(ctx, "Runtime.evaluate", raw)
	if err != nil {
		return "", fmt.Errorf("read page: %w", err)
	}

	// Parse the result to extract the evaluated value.
	var evalResult struct {
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		// Return raw result if we can't parse it.
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b), nil
	}
	if evalResult.Result.Type == "undefined" {
		return "", fmt.Errorf("read page: page script returned undefined (selector %q matched nothing?)", params.Selector)
	}

	// Truncate overly long page text to avoid blowing the tool output limit.
	text := evalResult.Result.Value
	if len(text) > MaxBodyLength {
		text = text[:MaxBodyLength] + fmt.Sprintf("\n\n[... truncated at %d characters; use a CSS selector to read a specific section]", MaxBodyLength)
	}

	return text, nil
}

// ─── browser_screenshot ────────────────────────────────────────────────────

type browserScreenshot struct{}

func (browserScreenshot) Name() string   { return "browser_screenshot" }
func (browserScreenshot) ReadOnly() bool { return true }
func (browserScreenshot) Description() string {
	return "Take a screenshot of the current page. Returns a base64-encoded image. PREFERRED format is JPEG for smaller size (use format='jpeg'). Use this to visually inspect the page layout, but prefer browser_read or browser_read_dom for text content since the AI can read text directly."
}

func (browserScreenshot) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "format":{"type":"string","description":"Image format: png (default) or jpeg","enum":["png","jpeg"]},
  "quality":{"type":"integer","description":"JPEG quality (0-100), default 80","minimum":0,"maximum":100}
},
"required":[]
}`)
}

func (browserScreenshot) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	text, _, err := browserScreenshot{}.ExecuteWithImages(ctx, args)
	return text, err
}

// ExecuteWithImages implements tool.ImageTool: the base64 payload rides the
// structural image channel (data: URL) so vision-capable providers embed it and
// the tool-output byte limiter never truncates it. Execute returns the same
// text with the placeholder marker.
func (browserScreenshot) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	var params struct {
		Format  string `json:"format"`
		Quality int    `json:"quality"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", nil, fmt.Errorf("invalid args: %w", err)
	}
	if params.Format == "" {
		params.Format = "png"
	}
	if params.Quality <= 0 {
		params.Quality = 80
	}

	screenshotParams, _ := json.Marshal(map[string]interface{}{
		"format":  params.Format,
		"quality": params.Quality,
	})
	result, err := browserrelay.Send(ctx, "Page.captureScreenshot", screenshotParams)
	if err != nil {
		return "", nil, fmt.Errorf("screenshot: %w", err)
	}

	var ssResult struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &ssResult); err != nil {
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b), nil, nil
	}
	if ssResult.Data == "" {
		return "", nil, fmt.Errorf("screenshot: empty image data")
	}

	mime := "image/png"
	if params.Format == "jpeg" {
		mime = "image/jpeg"
	}
	return "[image: " + mime + "]", []string{"data:" + mime + ";base64," + ssResult.Data}, nil
}

// ─── browser_eval ──────────────────────────────────────────────────────────

type browserEval struct{}

func (browserEval) Name() string   { return "browser_eval" }
func (browserEval) ReadOnly() bool { return false }
func (browserEval) Description() string {
	return "Execute arbitrary JavaScript in the current page context. Returns the result as a JSON string. Use this ONLY when other tools cannot achieve what you need — prefer browser_click, browser_fill_form, browser_read, etc. for standard operations. The expression must return a JSON-serializable value."
}

func (browserEval) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "expression":{"type":"string","description":"JavaScript expression to evaluate"}
},
"required":["expression"]
}`)
}

func (browserEval) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Expression == "" {
		return "", fmt.Errorf("expression is required")
	}

	raw := evalParams(params.Expression)
	result, err := browserrelay.Send(ctx, "Runtime.evaluate", raw)
	if err != nil {
		return "", fmt.Errorf("eval: %w", err)
	}
	if err := evaluateException(result); err != nil {
		return "", fmt.Errorf("eval: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ══════════════════════════════════════════════════════════════════════════
// New tools (tab management)
// ══════════════════════════════════════════════════════════════════════════

// ─── browser_list_pages ────────────────────────────────────────────────────

type browserListPages struct{}

func (browserListPages) Name() string   { return "browser_list_pages" }
func (browserListPages) ReadOnly() bool { return true }
func (browserListPages) Description() string {
	return "List all open browser tabs. Returns an array of pages with their index, tabId, title, URL, and active status. Use this first to discover what pages are open, then use browser_select_page to switch to a specific tab. PREFERRED tool for discovering open tabs."
}

func (browserListPages) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (browserListPages) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	result, err := browserrelay.SendTabCommand(ctx, "list_pages", nil)
	if err != nil {
		return "", fmt.Errorf("list pages: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_select_page ───────────────────────────────────────────────────

type browserSelectPage struct{}

func (browserSelectPage) Name() string   { return "browser_select_page" }
func (browserSelectPage) ReadOnly() bool { return false }
func (browserSelectPage) Description() string {
	return "Switch the browser debugger focus to a specific attached tab by its tabId. Call browser_attached_pages first to see which tabs are attached and their IDs. Only attached tabs can be selected."
}

func (browserSelectPage) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "tab_id":{"type":"integer","description":"The tab ID to switch to (from browser_list_pages)"}
},
"required":["tab_id"]
}`)
}

func (browserSelectPage) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		TabID int `json:"tab_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.TabID == 0 {
		return "", fmt.Errorf("tab_id is required")
	}

	result, err := browserrelay.SendTabCommandWithID(ctx, "select_page", params.TabID, nil)
	if err != nil {
		return "", fmt.Errorf("select page: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_attached_pages ───────────────────────────────────────────────

type browserAttachedPages struct{}

func (browserAttachedPages) Name() string   { return "browser_attached_pages" }
func (browserAttachedPages) ReadOnly() bool { return true }
func (browserAttachedPages) Description() string {
	return "List the browser tabs the user has explicitly attached to this session. Returns an array with tabId, URL, title, and which one is the active CDP target. Use this to know which pages you may operate on before calling browser_navigate/browser_click/etc."
}

func (browserAttachedPages) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (browserAttachedPages) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	result, err := browserrelay.SendTabCommand(ctx, "list_attached", nil)
	if err != nil {
		return "", fmt.Errorf("list attached pages: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_attach_page ──────────────────────────────────────────────────

type browserAttachPage struct{}

func (browserAttachPage) Name() string   { return "browser_attach_page" }
func (browserAttachPage) ReadOnly() bool { return false }
func (browserAttachPage) Description() string {
	return "Attach a browser tab by its tabId so it becomes readable and operable. The tab becomes the active CDP target. Use browser_list_pages first to find the tabId. Attaching grants read/operate access to that tab, so only attach tabs the user has approved."
}

func (browserAttachPage) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "tab_id":{"type":"integer","description":"The tab ID to attach (from browser_list_pages)"}
},
"required":["tab_id"]
}`)
}

func (browserAttachPage) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		TabID int `json:"tab_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.TabID == 0 {
		return "", fmt.Errorf("tab_id is required")
	}

	result, err := browserrelay.SendTabCommandWithID(ctx, "attach_page", params.TabID, nil)
	if err != nil {
		return "", fmt.Errorf("attach page: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_new_page ──────────────────────────────────────────────────────

type browserNewPage struct{}

func (browserNewPage) Name() string   { return "browser_new_page" }
func (browserNewPage) ReadOnly() bool { return false }
func (browserNewPage) Description() string {
	return "Open a new browser tab with an optional URL. The new tab becomes the active debugger target. Use this to open a new page without affecting the current tab. If no URL is given, opens about:blank."
}

func (browserNewPage) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "url":{"type":"string","description":"Optional URL to open in the new tab (default: about:blank)"}
},
"required":[]
}`)
}

func (browserNewPage) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		URL string `json:"url"`
	}
	json.Unmarshal(args, &params) // url is optional

	result, err := browserrelay.SendTabCommandWithURL(ctx, "new_page", params.URL, nil)
	if err != nil {
		return "", fmt.Errorf("new page: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_close_page ────────────────────────────────────────────────────

type browserClosePage struct{}

func (browserClosePage) Name() string   { return "browser_close_page" }
func (browserClosePage) ReadOnly() bool { return false }
func (browserClosePage) Description() string {
	return "Close a browser tab by its tabId. Use browser_list_pages first to discover the tab ID. The last open tab cannot be closed. If the closed tab was the active debugger target, another tab will be auto-attached."
}

func (browserClosePage) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "tab_id":{"type":"integer","description":"The tab ID to close (from browser_list_pages)"}
},
"required":["tab_id"]
}`)
}

func (browserClosePage) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		TabID int `json:"tab_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.TabID == 0 {
		return "", fmt.Errorf("tab_id is required")
	}

	result, err := browserrelay.SendTabCommandWithID(ctx, "close_page", params.TabID, nil)
	if err != nil {
		return "", fmt.Errorf("close page: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ══════════════════════════════════════════════════════════════════════════
// New tools (page interaction)
// ══════════════════════════════════════════════════════════════════════════

// ─── browser_read_dom ──────────────────────────────────────────────────────

type browserReadDOM struct{}

func (browserReadDOM) Name() string   { return "browser_read_dom" }
func (browserReadDOM) ReadOnly() bool { return true }
func (browserReadDOM) Description() string {
	return "Read all interactive elements on the current page (buttons, links, inputs, selects, textareas) with their CSS selectors, text content, and bounding rectangles. This is the PREFERRED tool for discovering what clickable elements exist on a page before using browser_click, browser_fill_form, or browser_type. Use this instead of browser_read when you need to interact with the page."
}

func (browserReadDOM) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (browserReadDOM) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	js := `(() => {
		const elements = [];
		const selectors = 'a, button, input, select, textarea, [role="button"], [role="link"], [tabindex]:not([tabindex="-1"])';
		document.querySelectorAll(selectors).forEach(el => {
			const rect = el.getBoundingClientRect();
			if (rect.width > 0 && rect.height > 0) {
				elements.push({
					tag: el.tagName.toLowerCase(),
					text: (el.textContent || '').trim().slice(0, 120),
					href: el.href || '',
					type: el.type || '',
					placeholder: el.placeholder || '',
					value: (el.value || '').slice(0, 80),
					rect: { x: Math.round(rect.x), y: Math.round(rect.y), w: Math.round(rect.width), h: Math.round(rect.height) },
					selector: (function(elem) {
						if (elem.id) return '#' + CSS.escape(elem.id);
						let path = [];
						while (elem && elem.nodeType === 1) {
							let sel = elem.tagName.toLowerCase();
							if (elem.id) { path.unshift('#' + CSS.escape(elem.id)); break; }
							if (elem.className && typeof elem.className === 'string') {
								const cls = elem.className.trim().split(/\\s+/).filter(c => c && !c.startsWith('_'));
								if (cls.length > 0) sel += '.' + cls.map(c => CSS.escape(c)).join('.');
							}
							const siblings = Array.from(elem.parentNode.children).filter(c => c.tagName === elem.tagName);
							if (siblings.length > 1) sel += ':nth-of-type(' + (Array.from(siblings).indexOf(elem) + 1) + ')';
							path.unshift(sel);
							elem = elem.parentElement;
						}
						return path.join(' > ');
					})(el)
				});
			}
		});
		return elements;
	})()`

	raw := evalParams(js)
	result, err := browserrelay.Send(ctx, "Runtime.evaluate", raw)
	if err != nil {
		return "", fmt.Errorf("read dom: %w", err)
	}
	if err := evaluateException(result); err != nil {
		return "", fmt.Errorf("read dom: %w", err)
	}

	var evalResult struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b), nil
	}

	b, _ := json.MarshalIndent(evalResult.Result.Value, "", "  ")
	return string(b), nil
}

// ─── browser_scroll ────────────────────────────────────────────────────────

type browserScroll struct{}

func (browserScroll) Name() string   { return "browser_scroll" }
func (browserScroll) ReadOnly() bool { return false }
func (browserScroll) Description() string {
	return "Scroll the page by a specified number of pixels, or scroll an element into view using a CSS selector. Use this when content is below the fold. PREFER using the selector parameter to scroll to a specific element rather than guessing pixel values."
}

func (browserScroll) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "selector":{"type":"string","description":"CSS selector of the element to scroll into view. PREFERRED: use this instead of guessing pixel values"},
  "direction":{"type":"string","description":"Scroll direction (only used when selector is empty)","enum":["up","down","left","right"]},
  "amount":{"type":"integer","description":"Pixels to scroll (only used when selector is empty; default 500)","default":500}
},
"required":[]
}`)
}

func (browserScroll) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Selector  string `json:"selector"`
		Direction string `json:"direction"`
		Amount    int    `json:"amount"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	if params.Selector != "" {
		// Preferred path: scroll the element into view (guide §9.2).
		js := fmt.Sprintf(`(()=>{
			const el = document.querySelector(%q);
			if (!el) throw new Error("element not found: "+%q);
			el.scrollIntoView({behavior:'smooth', block:'center'});
			return true;
		})()`, params.Selector, params.Selector)
		result, err := browserrelay.Send(ctx, "Runtime.evaluate", evalParams(js))
		if err != nil {
			return "", fmt.Errorf("scroll: %w", err)
		}
		if err := evaluateException(result); err != nil {
			return "", fmt.Errorf("scroll: %w", err)
		}
		return fmt.Sprintf(`{"scrolled": true, "selector": %q}`, params.Selector), nil
	}

	if params.Direction == "" {
		return "", fmt.Errorf("direction or selector is required")
	}
	if params.Amount <= 0 {
		params.Amount = 500
	}

	var dx, dy int
	switch params.Direction {
	case "up":
		dy = -params.Amount
	case "down":
		dy = params.Amount
	case "left":
		dx = -params.Amount
	case "right":
		dx = params.Amount
	default:
		return "", fmt.Errorf("invalid direction: %s (use up/down/left/right)", params.Direction)
	}

	js := fmt.Sprintf(`window.scrollBy({top: %d, left: %d, behavior: 'smooth'}); window.scrollY`, dy, dx)
	raw := evalParams(js)
	if _, err := browserrelay.Send(ctx, "Runtime.evaluate", raw); err != nil {
		return "", fmt.Errorf("scroll: %w", err)
	}

	return fmt.Sprintf(`{"scrolled": true, "direction": %q, "amount": %d}`, params.Direction, params.Amount), nil
}

// ─── browser_go_back ───────────────────────────────────────────────────────

type browserGoBack struct{}

func (browserGoBack) Name() string   { return "browser_go_back" }
func (browserGoBack) ReadOnly() bool { return false }
func (browserGoBack) Description() string {
	return "Navigate back to the previous page in browser history. Equivalent to clicking the browser back button. Use browser_read after navigating to confirm the page changed."
}

func (browserGoBack) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (browserGoBack) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	// Get navigation history to find the correct entry ID.
	historyResult, err := browserrelay.Send(ctx, "Page.getNavigationHistory", nil)
	if err != nil {
		return "", fmt.Errorf("go back: get history: %w", err)
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(historyResult, &history); err != nil {
		return "", fmt.Errorf("go back: parse history: %w", err)
	}
	if history.CurrentIndex <= 0 {
		return "", fmt.Errorf("go back: no previous page in history")
	}
	entryID := history.Entries[history.CurrentIndex-1].ID
	raw, _ := json.Marshal(map[string]int{"entryId": entryID})
	result, err := browserrelay.Send(ctx, "Page.navigateToHistoryEntry", raw)
	if err != nil {
		return "", fmt.Errorf("go back: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_go_forward ────────────────────────────────────────────────────

type browserGoForward struct{}

func (browserGoForward) Name() string   { return "browser_go_forward" }
func (browserGoForward) ReadOnly() bool { return false }
func (browserGoForward) Description() string {
	return "Navigate forward to the next page in browser history. Equivalent to clicking the browser forward button. Only works after browser_go_back. Use browser_read to confirm the page changed."
}

func (browserGoForward) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (browserGoForward) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	// Get navigation history to find the correct entry ID.
	historyResult, err := browserrelay.Send(ctx, "Page.getNavigationHistory", nil)
	if err != nil {
		return "", fmt.Errorf("go forward: get history: %w", err)
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(historyResult, &history); err != nil {
		return "", fmt.Errorf("go forward: parse history: %w", err)
	}
	if history.CurrentIndex >= len(history.Entries)-1 {
		return "", fmt.Errorf("go forward: no next page in history")
	}
	entryID := history.Entries[history.CurrentIndex+1].ID
	raw, _ := json.Marshal(map[string]int{"entryId": entryID})
	result, err := browserrelay.Send(ctx, "Page.navigateToHistoryEntry", raw)
	if err != nil {
		return "", fmt.Errorf("go forward: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_press_key ─────────────────────────────────────────────────────

type browserPressKey struct{}

func (browserPressKey) Name() string   { return "browser_press_key" }
func (browserPressKey) ReadOnly() bool { return false }
func (browserPressKey) Description() string {
	return "Press a key or key combination in the currently focused element. Use this AFTER typing text with browser_fill_form or browser_type to submit forms (Enter), navigate (Tab, Escape), or trigger keyboard shortcuts (Control+A, Control+C, Shift+Tab). Supports: Enter, Tab, Escape, ArrowUp/Down/Left/Right, Backspace, Delete, Home, End, F1-F12, and modifier combinations."
}

func (browserPressKey) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "key":{"type":"string","description":"Key to press (Enter, Escape, Tab, ArrowUp, etc.)"}
},
"required":["key"]
}`)
}

func (browserPressKey) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Key == "" {
		return "", fmt.Errorf("key is required")
	}

	// Split modifier prefixes ("Control+A") from the terminal key ("a"). CDP
	// expects the DOM key in `key` and the modifier state as a bitmask.
	key, modifiers := splitModifiers(params.Key)

	keyDown, _ := json.Marshal(map[string]interface{}{
		"type":      "keyDown",
		"key":       key,
		"modifiers": modifiers,
	})
	if _, err := browserrelay.Send(ctx, "Input.dispatchKeyEvent", keyDown); err != nil {
		return "", fmt.Errorf("press key down: %w", err)
	}

	keyUp, _ := json.Marshal(map[string]interface{}{
		"type":      "keyUp",
		"key":       key,
		"modifiers": modifiers,
	})
	if _, err := browserrelay.Send(ctx, "Input.dispatchKeyEvent", keyUp); err != nil {
		return "", fmt.Errorf("press key up: %w", err)
	}

	return fmt.Sprintf(`{"pressed": true, "key": %q}`, params.Key), nil
}

// splitModifiers parses key names that carry modifier prefixes (Control+A,
// Shift+Tab, Alt+ArrowUp) into the CDP key and modifiers bitmask (Alt=1,
// Ctrl=2, Meta=4, Shift=8). A bare key passes through unchanged.
func splitModifiers(name string) (key string, modifiers int) {
	key = name
	for _, part := range strings.Split(name, "+") {
		switch strings.ToLower(part) {
		case "ctrl", "control":
			modifiers |= 2
		case "shift":
			modifiers |= 8
		case "alt", "option":
			modifiers |= 1
		case "meta", "cmd", "command", "super":
			modifiers |= 4
		default:
			key = part // terminal key: last non-modifier segment wins
		}
	}
	if key == "" || key == name {
		return name, modifiers // no modifier prefix found (or bare modifier)
	}
	return key, modifiers
}

// ─── browser_hover ─────────────────────────────────────────────────────────

type browserHover struct{}

func (browserHover) Name() string   { return "browser_hover" }
func (browserHover) ReadOnly() bool { return false }
func (browserHover) Description() string {
	return "Hover the mouse over an element identified by a CSS selector. Use this to trigger hover effects, tooltips, or dropdown menus that appear on hover. After hovering, use browser_read_dom to discover newly visible elements. The element is scrolled into view before hovering."
}

func (browserHover) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "selector":{"type":"string","description":"CSS selector for the element to hover over"}
},
"required":["selector"]
}`)
}

func (browserHover) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Selector == "" {
		return "", fmt.Errorf("selector is required")
	}

	js := fmt.Sprintf(`(()=>{
		const el = document.querySelector(%q);
		if (!el) throw new Error("element not found: "+%q);
		const rect = el.getBoundingClientRect();
		return {x: Math.round(rect.x + rect.width/2), y: Math.round(rect.y + rect.height/2)};
	})()`, params.Selector, params.Selector)

	coordRaw := evalParams(js)
	coordResult, err := browserrelay.Send(ctx, "Runtime.evaluate", coordRaw)
	if err != nil {
		return "", fmt.Errorf("get coordinates: %w", err)
	}
	if err := evaluateException(coordResult); err != nil {
		return "", fmt.Errorf("hover: %w", err)
	}

	var evalResult struct {
		Result struct {
			Type  string `json:"type"`
			Value struct {
				X int `json:"x"`
				Y int `json:"y"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(coordResult, &evalResult); err != nil || evalResult.Result.Type != "object" {
		return "", fmt.Errorf("hover: element found but could not extract coordinates (selector=%q, parse_err=%v)", params.Selector, err)
	}

	x, y := evalResult.Result.Value.X, evalResult.Result.Value.Y
	moveParams, _ := json.Marshal(map[string]interface{}{
		"type": "mouseMoved",
		"x":    x,
		"y":    y,
	})
	if _, err := browserrelay.Send(ctx, "Input.dispatchMouseEvent", moveParams); err != nil {
		return "", fmt.Errorf("hover: %w", err)
	}

	return fmt.Sprintf(`{"hovered": true, "selector": %q, "x": %d, "y": %d}`, params.Selector, x, y), nil
}

// ─── browser_wait ──────────────────────────────────────────────────────────

type browserWait struct{}

func (browserWait) Name() string   { return "browser_wait" }
func (browserWait) ReadOnly() bool { return true }
func (browserWait) Description() string {
	return "Wait for an element to appear on the page, identified by a CSS selector. Use this after browser_navigate or browser_click to wait for dynamic content to load. By default waits up to 5 seconds and checks if the element is visible. Returns true if found, false if timeout. PREFER this over fixed delays."
}

func (browserWait) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "selector":{"type":"string","description":"CSS selector to wait for"},
  "timeout":{"type":"integer","description":"Maximum wait time in milliseconds (default 5000)"},
  "state":{"type":"string","description":"Element state to wait for","enum":["visible","present"],"default":"visible"}
},
"required":["selector"]
}`)
}

func (browserWait) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Selector string `json:"selector"`
		Timeout  int    `json:"timeout"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Selector == "" {
		return "", fmt.Errorf("selector is required")
	}
	if params.Timeout <= 0 {
		params.Timeout = 5000
	}
	if params.State == "" {
		params.State = "visible"
	}

	checkVisible := params.State == "visible"
	// Runtime.evaluate runs a script, not a module: a top-level `await` is a
	// syntax error. Wrap the polling loop in an immediately-invoked promise so
	// awaitPromise: true can wait on it.
	js := fmt.Sprintf(`(async () => {
		const start = Date.now();
		const timeout = %d;
		const checkVisible = %v;
		const selector = %q;
		function poll() {
			const el = document.querySelector(selector);
			if (el && (!checkVisible || el.offsetParent !== null)) {
				return JSON.stringify({found: true, text: (el.textContent || '').trim().slice(0, 500)});
			}
			if (Date.now() - start > timeout) {
				return JSON.stringify({found: false, timeout: true, message: 'Element not found within ' + timeout + 'ms'});
			}
			return new Promise((resolve) => setTimeout(() => resolve(poll()), 200));
		}
		return poll();
	})()`, params.Timeout, checkVisible, params.Selector)

	raw := evalParams(js, map[string]interface{}{"awaitPromise": true})
	result, err := browserrelay.Send(ctx, "Runtime.evaluate", raw)
	if err != nil {
		return "", fmt.Errorf("wait: %w", err)
	}
	if err := evaluateException(result); err != nil {
		return "", fmt.Errorf("wait: %w", err)
	}

	var evalResult struct {
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b), nil
	}

	return evalResult.Result.Value, nil
}

// ─── browser_upload_file ───────────────────────────────────────────────────

type browserUploadFile struct{}

func (browserUploadFile) Name() string   { return "browser_upload_file" }
func (browserUploadFile) ReadOnly() bool { return false }
func (browserUploadFile) Description() string {
	return "Upload a file to a file input element on the page. The file path must be an absolute path on the local machine. Use this for file upload forms. Requires the page to have a file input element."
}

func (browserUploadFile) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "selector":{"type":"string","description":"CSS selector for the file input element"},
  "file_path":{"type":"string","description":"Absolute path to the file to upload"}
},
"required":["selector","file_path"]
}`)
}

func (browserUploadFile) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Selector string `json:"selector"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Selector == "" || params.FilePath == "" {
		return "", fmt.Errorf("selector and file_path are required")
	}

	// Use CDP's DOM.setFileInputFiles to set the file.
	docRaw, _ := json.Marshal(map[string]interface{}{})
	doc, err := browserrelay.Send(ctx, "DOM.getDocument", docRaw)
	if err != nil {
		return "", fmt.Errorf("get document: %w", err)
	}

	var docResult struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(doc, &docResult); err != nil {
		return "", fmt.Errorf("parse document: %w", err)
	}

	qRaw, _ := json.Marshal(map[string]interface{}{
		"nodeId":   docResult.Root.NodeID,
		"selector": params.Selector,
	})
	qResult, err := browserrelay.Send(ctx, "DOM.querySelector", qRaw)
	if err != nil {
		return "", fmt.Errorf("query selector: %w", err)
	}

	var qRes struct {
		NodeID int `json:"nodeId"`
	}
	if err := json.Unmarshal(qResult, &qRes); err != nil || qRes.NodeID == 0 {
		return "", fmt.Errorf("element not found: %s", params.Selector)
	}

	fileRaw, _ := json.Marshal(map[string]interface{}{
		"nodeId": qRes.NodeID,
		"files":  []string{params.FilePath},
	})
	if _, err := browserrelay.Send(ctx, "DOM.setFileInputFiles", fileRaw); err != nil {
		return "", fmt.Errorf("set file: %w", err)
	}

	return fmt.Sprintf(`{"uploaded": true, "selector": %q, "file": %q}`, params.Selector, params.FilePath), nil
}

// ─── browser_resize ────────────────────────────────────────────────────────

type browserResize struct{}

func (browserResize) Name() string   { return "browser_resize" }
func (browserResize) ReadOnly() bool { return false }
func (browserResize) Description() string {
	return "Resize the page viewport to the specified width and height in pixels. Use this to test responsive layouts or to ensure the page is rendered at a specific size before taking a screenshot. Default device scale factor is 1."
}

func (browserResize) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "width":{"type":"integer","description":"Viewport width in pixels (default 1280)"},
  "height":{"type":"integer","description":"Viewport height in pixels (default 720)"}
},
"required":["width","height"]
}`)
}

func (browserResize) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Width <= 0 || params.Height <= 0 {
		return "", fmt.Errorf("width and height are required")
	}

	emulationParams, _ := json.Marshal(map[string]interface{}{
		"width":             params.Width,
		"height":            params.Height,
		"deviceScaleFactor": 1,
		"mobile":            false,
	})
	if _, err := browserrelay.Send(ctx, "Emulation.setDeviceMetricsOverride", emulationParams); err != nil {
		return "", fmt.Errorf("resize: %w", err)
	}

	return fmt.Sprintf(`{"resized": true, "width": %d, "height": %d}`, params.Width, params.Height), nil
}

// ─── browser_handle_dialog ─────────────────────────────────────────────────

type browserHandleDialog struct{}

func (browserHandleDialog) Name() string   { return "browser_handle_dialog" }
func (browserHandleDialog) ReadOnly() bool { return false }
func (browserHandleDialog) Description() string {
	return "Accept or dismiss a JavaScript dialog (alert, confirm, or prompt). By default accepts the dialog. For prompt dialogs, provide the text to enter. Use this when a dialog is blocking page interaction — browser_click and other tools will fail until the dialog is handled."
}

func (browserHandleDialog) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "accept":{"type":"boolean","description":"Accept (true) or dismiss (false) the dialog (default true)"},
  "text":{"type":"string","description":"Text to enter for prompt dialogs"}
},
"required":[]
}`)
}

func (browserHandleDialog) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Accept *bool  `json:"accept"` // nil = omitted; default true
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	accept := true // documented default: accept when omitted
	if params.Accept != nil {
		accept = *params.Accept
	}

	dialogParams := map[string]interface{}{
		"accept": accept,
	}
	if params.Text != "" {
		dialogParams["promptText"] = params.Text
	}

	raw, _ := json.Marshal(dialogParams)
	if _, err := browserrelay.Send(ctx, "Page.handleJavaScriptDialog", raw); err != nil {
		return "", fmt.Errorf("handle dialog: %w", err)
	}

	action := "accepted"
	if !accept {
		action = "dismissed"
	}
	return fmt.Sprintf(`{"dialog": %q}`, action), nil
}

// ─── browser_fill_form ─────────────────────────────────────────────────────

type browserFillForm struct{}

func (browserFillForm) Name() string   { return "browser_fill_form" }
func (browserFillForm) ReadOnly() bool { return false }
func (browserFillForm) Description() string {
	return "Fill multiple form fields at once by providing a map of CSS selectors to values. This is the PREFERRED tool for filling forms — it's faster and more reliable than calling browser_fill_form repeatedly per field. Handles inputs, textareas, select dropdowns, checkboxes, and radio buttons in one call."
}

func (browserFillForm) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "fields":{"type":"object","description":"Map of CSS selector to value, e.g. {\"#name\": \"John\", \"#email\": \"john@test.com\"}"}
},
"required":["fields"]
}`)
}

func (browserFillForm) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if len(params.Fields) == 0 {
		return "", fmt.Errorf("fields is required")
	}

	// Build JS that fills all fields at once.
	js := `(() => { const results = {}; `
	for selector, value := range params.Fields {
		js += fmt.Sprintf(`try {
			const el = document.querySelector(%q);
			if (el) {
				el.focus();
				const type = (el.type || '').toLowerCase();
				if (type === 'checkbox' || type === 'radio') {
					// Assigning .value never toggles checked state; parse an
					// explicit boolean representation instead.
					const on = ['true', '1', 'on', 'yes', 'checked'].includes(String(%q).toLowerCase());
					el.checked = on;
				} else {
					el.value = %q;
					el.dispatchEvent(new Event('input', {bubbles:true}));
					el.dispatchEvent(new Event('change', {bubbles:true}));
				}
				results[%q] = 'ok';
			} else {
				results[%q] = 'not found';
			}
		} catch(e) { results[%q] = e.message; }`, selector, value, value, selector, selector, selector)
	}
	js += ` return JSON.stringify(results); })()`

	raw := evalParams(js)
	result, err := browserrelay.Send(ctx, "Runtime.evaluate", raw)
	if err != nil {
		return "", fmt.Errorf("fill form: %w", err)
	}
	if err := evaluateException(result); err != nil {
		return "", fmt.Errorf("fill form: %w", err)
	}

	var evalResult struct {
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &evalResult); err != nil {
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b), nil
	}

	return evalResult.Result.Value, nil
}

// ─── browser_emulate ───────────────────────────────────────────────────────

type browserEmulate struct{}

func (browserEmulate) Name() string   { return "browser_emulate" }
func (browserEmulate) ReadOnly() bool { return false }
func (browserEmulate) Description() string {
	return "Emulate a device viewport on the current page. Use this to test responsive layouts by setting viewport width, height, device scale factor, and mobile mode. Call browser_resize to reset to default."
}

func (browserEmulate) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "width":{"type":"integer","description":"Viewport width in pixels (default 375)"},
  "height":{"type":"integer","description":"Viewport height in pixels (default 812)"},
  "device_scale_factor":{"type":"number","description":"Device scale factor (default 2)"},
  "mobile":{"type":"boolean","description":"Whether to emulate mobile viewport (default true)"}
},
"required":[]
}`)
}

func (browserEmulate) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Width             int     `json:"width"`
		Height            int     `json:"height"`
		DeviceScaleFactor float64 `json:"device_scale_factor"`
		Mobile            *bool   `json:"mobile"` // nil = omitted; default true
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.Width == 0 {
		params.Width = 375
	}
	if params.Height == 0 {
		params.Height = 812
	}
	if params.DeviceScaleFactor == 0 {
		params.DeviceScaleFactor = 2
	}
	mobile := true // documented default: emulate mobile viewport when omitted
	if params.Mobile != nil {
		mobile = *params.Mobile
	}

	raw, _ := json.Marshal(map[string]interface{}{
		"width":             params.Width,
		"height":            params.Height,
		"deviceScaleFactor": params.DeviceScaleFactor,
		"mobile":            mobile,
	})
	result, err := browserrelay.Send(ctx, "Emulation.setDeviceMetricsOverride", raw)
	if err != nil {
		return "", fmt.Errorf("emulate: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_take_snapshot ──────────────────────────────────────────────────

type browserTakeSnapshot struct{}

func (browserTakeSnapshot) Name() string   { return "browser_take_snapshot" }
func (browserTakeSnapshot) ReadOnly() bool { return true }
func (browserTakeSnapshot) Description() string {
	return "Take a text snapshot of the current page based on the accessibility tree. Returns structured elements with roles, names, and unique IDs. Use this to discover page structure and interactive elements when browser_read_dom is not detailed enough."
}

func (browserTakeSnapshot) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (browserTakeSnapshot) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	result, err := browserrelay.Send(ctx, "Accessibility.getFullAXTree", nil)
	if err != nil {
		return "", fmt.Errorf("take snapshot: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_drag ───────────────────────────────────────────────────────────

type browserDrag struct{}

func (browserDrag) Name() string   { return "browser_drag" }
func (browserDrag) ReadOnly() bool { return false }
func (browserDrag) Description() string {
	return "Drag an element onto another element. Uses CDP Input.dispatchMouseEvent to simulate mouse down, move, and up. Provide the source CSS selector (what to drag) and the target CSS selector (where to drop)."
}

func (browserDrag) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "from_selector":{"type":"string","description":"CSS selector of the element to drag"},
  "to_selector":{"type":"string","description":"CSS selector of the drop target element"}
},
"required":["from_selector","to_selector"]
}`)
}

func (browserDrag) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		FromSelector string `json:"from_selector"`
		ToSelector   string `json:"to_selector"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if params.FromSelector == "" || params.ToSelector == "" {
		return "", fmt.Errorf("from_selector and to_selector are required")
	}

	// Get coordinates of source and target elements.
	js := fmt.Sprintf(`JSON.stringify({
		from: (() => { const el = document.querySelector(%q); const r = el.getBoundingClientRect(); return {x: r.x + r.width/2, y: r.y + r.height/2}; })(),
		to:   (() => { const el = document.querySelector(%q); const r = el.getBoundingClientRect(); return {x: r.x + r.width/2, y: r.y + r.height/2}; })()
	})`, params.FromSelector, params.ToSelector)
	raw := evalParams(js)
	coordResult, err := browserrelay.Send(ctx, "Runtime.evaluate", raw)
	if err != nil {
		return "", fmt.Errorf("drag: get coordinates: %w", err)
	}
	if err := evaluateException(coordResult); err != nil {
		return "", fmt.Errorf("drag: get coordinates: %w", err)
	}
	var evalResult struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(coordResult, &evalResult); err != nil {
		return "", fmt.Errorf("drag: parse coordinates: %w", err)
	}
	var coords struct {
		From struct{ X, Y float64 } `json:"from"`
		To   struct{ X, Y float64 } `json:"to"`
	}
	if err := json.Unmarshal([]byte(evalResult.Result.Value), &coords); err != nil {
		return "", fmt.Errorf("drag: parse coordinates: %w", err)
	}

	// Step 1: mousedown at source.
	downRaw, _ := json.Marshal(map[string]interface{}{
		"type":       "mousePressed",
		"x":          coords.From.X,
		"y":          coords.From.Y,
		"button":     "left",
		"clickCount": 1,
	})
	if _, err := browserrelay.Send(ctx, "Input.dispatchMouseEvent", downRaw); err != nil {
		return "", fmt.Errorf("drag: mousedown: %w", err)
	}

	// Step 2: mousemove to target.
	moveRaw, _ := json.Marshal(map[string]interface{}{
		"type": "mouseMoved",
		"x":    coords.To.X,
		"y":    coords.To.Y,
	})
	if _, err := browserrelay.Send(ctx, "Input.dispatchMouseEvent", moveRaw); err != nil {
		return "", fmt.Errorf("drag: mousemove: %w", err)
	}

	// Step 3: mouseup at target.
	upRaw, _ := json.Marshal(map[string]interface{}{
		"type":       "mouseReleased",
		"x":          coords.To.X,
		"y":          coords.To.Y,
		"button":     "left",
		"clickCount": 1,
	})
	if _, err := browserrelay.Send(ctx, "Input.dispatchMouseEvent", upRaw); err != nil {
		return "", fmt.Errorf("drag: mouseup: %w", err)
	}

	return fmt.Sprintf(`{"dragged": true, "from_x": %g, "from_y": %g, "to_x": %g, "to_y": %g}`, coords.From.X, coords.From.Y, coords.To.X, coords.To.Y), nil
}

// ─── browser_list_console_messages ──────────────────────────────────────────

type browserListConsoleMessages struct{}

func (browserListConsoleMessages) Name() string   { return "browser_list_console_messages" }
func (browserListConsoleMessages) ReadOnly() bool { return true }
func (browserListConsoleMessages) Description() string {
	return "List console messages logged by the current page since the last navigation. Returns an array of messages with level, text, source, and line number. Use this to debug JavaScript errors, warnings, and logs. Pass clear=true to clear the cache after reading."
}

func (browserListConsoleMessages) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "clear":{"type":"boolean","description":"Clear the console message cache after reading"}
},
"required":[]
}`)
}

func (browserListConsoleMessages) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Clear bool `json:"clear"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var p json.RawMessage
	if params.Clear {
		p, _ = json.Marshal(map[string]bool{"clear": true})
	}
	result, err := browserrelay.SendTabCommand(ctx, "list_console_messages", p)
	if err != nil {
		return "", fmt.Errorf("list console messages: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}

// ─── browser_list_network_requests ──────────────────────────────────────────

type browserListNetworkRequests struct{}

func (browserListNetworkRequests) Name() string   { return "browser_list_network_requests" }
func (browserListNetworkRequests) ReadOnly() bool { return true }
func (browserListNetworkRequests) Description() string {
	return "List network requests made by the current page. Returns an array of requests with URL, method, status code, type, and headers. Use this to see what API calls the page is making, debug failed requests, or inspect request/response details. Pass clear=true to clear the cache after reading."
}

func (browserListNetworkRequests) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "clear":{"type":"boolean","description":"Clear the network request cache after reading"}
},
"required":[]
}`)
}

func (browserListNetworkRequests) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Clear bool `json:"clear"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	var p json.RawMessage
	if params.Clear {
		p, _ = json.Marshal(map[string]bool{"clear": true})
	}
	result, err := browserrelay.SendTabCommand(ctx, "list_network_requests", p)
	if err != nil {
		return "", fmt.Errorf("list network requests: %w", err)
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), nil
}
