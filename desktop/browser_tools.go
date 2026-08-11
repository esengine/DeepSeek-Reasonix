// Agent browser tools: the fixed browser_read / browser_act pair injected only
// into local desktop controllers via boot.Options.HostTools. Their schemas are
// byte-stable for the whole session and never depend on whether the Browser
// Companion is installed or running — an unavailable companion surfaces as a
// typed execution error, so the provider-visible tool surface cannot drift.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/tool"

	"reasonix/desktop/internal/browseripc"
)

// browserReadSchema / browserActSchema are the canonical tool schemas. They are
// part of the provider-visible prefix for desktop sessions: any change must be
// deliberate and pinned by TestBrowserToolSchemasGolden.
const browserReadSchema = `{
	"type":"object",
	"properties":{
		"action":{"type":"string","enum":["list_tabs","open","navigate","snapshot","screenshot","wait"],"description":"list_tabs: enumerate the chat's tabs; open: open a new tab; navigate: load a URL in a tab; snapshot: capture a compact page view with refs; screenshot: capture a page image; wait: wait for a page state."},
		"tab_id":{"type":"string","description":"A tab from a previous list_tabs/open/snapshot result. Omit for list_tabs and open."},
		"url":{"type":"string","description":"Absolute http(s) URL. Only used by open and navigate."},
		"wait_until":{"type":"string","enum":["load","network_idle","dom_content_loaded","navigation"],"description":"Only used by wait."},
		"timeout_ms":{"type":"integer","description":"Only used by wait."},
		"max_chars":{"type":"integer","description":"Only used by snapshot; caps returned text (default 50000)."}
	},
	"required":["action"]
}`

const browserActSchema = `{
	"type":"object",
	"properties":{
		"action":{"type":"string","enum":["click","hover","scroll","type","press","select"],"description":"One restricted input primitive. There is no arbitrary JavaScript execution."},
		"tab_id":{"type":"string","description":"A tab from a previous list_tabs/open/snapshot result."},
		"expected_origin":{"type":"string","description":"The exact origin (scheme://host:port) the tab must be on when the action runs. Pass the origin from the snapshot that produced ref; the action fails if the tab moved."},
		"ref":{"type":"string","description":"An opaque node ref from a snapshot. Required for click and hover; navigation invalidates all previous refs."},
		"text":{"type":"string","description":"Only used by type."},
		"key":{"type":"string","description":"Only used by press (e.g. Enter, Tab, Escape)."},
		"value":{"type":"string","description":"Only used by select (option value)."},
		"delta":{"type":"integer","description":"Only used by scroll (pixels, negative scrolls up)."}
	},
	"required":["action","tab_id"]
}`

// browserReadTool executes the read-only browser surface. It is logically
// read-only and allowed in Plan mode, but starting the companion is a host
// mutation, so strict read-only agents reject it.
type browserReadTool struct {
	ownerID func() string
	app     *App
}

func (b *browserReadTool) Name() string { return "browser_read" }
func (b *browserReadTool) Description() string {
	return "Read-only browser access: list the chat's tabs, open or navigate a tab, snapshot a compact page view, screenshot the page, or wait for a page state. All actions are scoped to the current chat."
}
func (b *browserReadTool) Schema() json.RawMessage {
	return json.RawMessage(browserReadSchema)
}
func (b *browserReadTool) ReadOnly() bool { return true }
func (b *browserReadTool) PlanModeSafe() bool {
	return true
}
func (b *browserReadTool) ReadOnlyExecutionHostMutation() bool { return true }

func (b *browserReadTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return b.execute(ctx, args)
}

func (b *browserReadTool) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	var p struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(args, &p)
	if p.Action == "screenshot" {
		text, images, err := b.screenshot(ctx, args)
		return text, images, err
	}
	text, err := b.execute(ctx, args)
	return text, nil, err
}

func (b *browserReadTool) execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Action    string `json:"action"`
		TabID     string `json:"tab_id"`
		URL       string `json:"url"`
		WaitUntil string `json:"wait_until"`
		TimeoutMs *int   `json:"timeout_ms"`
		MaxChars  *int   `json:"max_chars"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	ownerID := b.ownerID()
	if ownerID == "" {
		return "", fmt.Errorf("browser tools are unavailable: no active chat binding")
	}
	switch p.Action {
	case "list_tabs":
		var res browseripc.TabListResult
		if err := b.app.browser.Call(ctx, ownerID, "tab.list", browseripc.OwnerParams{OwnerID: ownerID}, &res); err != nil {
			return "", browserToolError(err)
		}
		if len(res.Tabs) == 0 {
			return "No tabs are open in this chat. Use action \"open\" to open a URL.", nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d tab(s) in this chat:\n", len(res.Tabs))
		for _, t := range res.Tabs {
			marker := " "
			if t.Active {
				marker = "*"
			}
			fmt.Fprintf(&sb, "%s %s %s\n", marker, t.TabID, t.URL)
		}
		return sb.String(), nil
	case "open":
		if p.URL == "" {
			return "", fmt.Errorf("open requires url")
		}
		var res browseripc.TabInfo
		if err := b.app.browser.Call(ctx, ownerID, "tab.open", browseripc.TabOpenParams{
			OwnerID: ownerID, URL: p.URL, Disposition: browseripc.DispositionForeground, FromAgent: true,
		}, &res); err != nil {
			return "", browserToolError(err)
		}
		return fmt.Sprintf("opened %s in tab %s (generation %d)", res.URL, res.TabID, res.Generation), nil
	case "navigate":
		if p.TabID == "" || p.URL == "" {
			return "", fmt.Errorf("navigate requires tab_id and url")
		}
		var res browseripc.TabNavigateResult
		if err := b.app.browser.Call(ctx, ownerID, "tab.navigate", browseripc.TabNavigateParams{
			OwnerID: ownerID, TabID: p.TabID, URL: p.URL,
		}, &res); err != nil {
			return "", browserToolError(err)
		}
		return fmt.Sprintf("navigating tab %s to %s (generation %d)", res.TabID, res.URL, res.Generation), nil
	case "snapshot":
		if p.TabID == "" {
			return "", fmt.Errorf("snapshot requires tab_id")
		}
		maxChars := browseripc.MaxTextChars
		if p.MaxChars != nil && *p.MaxChars > 0 && *p.MaxChars < maxChars {
			maxChars = *p.MaxChars
		}
		var res browseripc.TabSnapshotResult
		if err := b.app.browser.Call(ctx, ownerID, "tab.snapshot", browseripc.TabSnapshotParams{
			OwnerID: ownerID, TabID: p.TabID, MaxChars: &maxChars,
		}, &res); err != nil {
			return "", browserToolError(err)
		}
		return browserSnapshotText(&res), nil
	case "wait":
		if p.TabID == "" {
			return "", fmt.Errorf("wait requires tab_id")
		}
		waitUntil := browseripc.WaitLoad
		switch p.WaitUntil {
		case "network_idle":
			waitUntil = browseripc.WaitNetworkIdle
		case "dom_content_loaded":
			waitUntil = browseripc.WaitDOMContent
		case "navigation":
			waitUntil = browseripc.WaitNavigation
		}
		var res browseripc.TabWaitResult
		if err := b.app.browser.Call(ctx, ownerID, "tab.wait", browseripc.TabWaitParams{
			OwnerID: ownerID, TabID: p.TabID, WaitUntil: waitUntil, TimeoutMs: p.TimeoutMs,
		}, &res); err != nil {
			return "", browserToolError(err)
		}
		return fmt.Sprintf("tab %s is at %s (title: %s, generation %d)", res.TabID, res.URL, res.Title, res.Generation), nil
	default:
		return "", fmt.Errorf("unknown browser_read action %q", p.Action)
	}
}

// screenshot captures the page image and sends it through the structural
// ImageTool channel; the text result only carries dimensions and provenance.
func (b *browserReadTool) screenshot(ctx context.Context, args json.RawMessage) (string, []string, error) {
	var p struct {
		TabID string `json:"tab_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", nil, fmt.Errorf("invalid args: %w", err)
	}
	if p.TabID == "" {
		return "", nil, fmt.Errorf("screenshot requires tab_id")
	}
	var res browseripc.TabScreenshotResult
	if err := b.app.browser.Call(ctx, b.ownerID(), "tab.screenshot", browseripc.TabScreenshotParams{
		OwnerID: b.ownerID(), TabID: p.TabID,
	}, &res); err != nil {
		return "", nil, browserToolError(err)
	}
	text := fmt.Sprintf("screenshot of tab %s (%dx%d): %s (title: %s)", res.TabID, res.Width, res.Height, res.URL, res.Title)
	return text, []string{res.ImageDataURL}, nil
}

// browserActTool performs one restricted input primitive. It is a writer tool:
// forbidden in Plan mode, and every action verifies expected_origin against
// the tab's current origin before touching the page.
type browserActTool struct {
	ownerID func() string
	app     *App
}

func (b *browserActTool) Name() string { return "browser_act" }
func (b *browserActTool) Description() string {
	return "Perform one restricted input action in the built-in browser: click, hover, scroll, type, press, or select. The action is scoped to the current chat and runs only when the tab is on the exact expected_origin from the snapshot that produced ref."
}
func (b *browserActTool) Schema() json.RawMessage {
	return json.RawMessage(browserActSchema)
}
func (b *browserActTool) ReadOnly() bool { return false }

func (b *browserActTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Action         string `json:"action"`
		TabID          string `json:"tab_id"`
		ExpectedOrigin string `json:"expected_origin"`
		Ref            string `json:"ref"`
		Text           string `json:"text"`
		Key            string `json:"key"`
		Value          string `json:"value"`
		Delta          *int   `json:"delta"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.TabID == "" {
		return "", fmt.Errorf("browser_act requires tab_id")
	}
	switch p.Action {
	case "click", "hover", "scroll", "type", "press", "select":
	default:
		return "", fmt.Errorf("unknown browser_act action %q", p.Action)
	}
	ownerID := b.ownerID()
	if ownerID == "" {
		return "", fmt.Errorf("browser tools are unavailable: no active chat binding")
	}
	params := browseripc.TabActParams{
		OwnerID:        ownerID,
		TabID:          p.TabID,
		ExpectedOrigin: p.ExpectedOrigin,
		Action:         browseripc.ActAction(p.Action),
		Ref:            p.Ref,
		Text:           p.Text,
		Key:            p.Key,
		Value:          p.Value,
		Delta:          p.Delta,
	}
	var res browseripc.TabActResult
	if err := b.app.browser.Call(ctx, ownerID, "tab.act", params, &res); err != nil {
		return "", browserToolError(err)
	}
	return fmt.Sprintf("%s on tab %s done (now at %s, generation %d)", p.Action, res.TabID, res.URL, res.Generation), nil
}

// browserHostToolsForTab returns the fixed browser tool pair bound to the
// chat's tab ID. The schemas are constant and never change with companion
// availability; only execution results vary. Local desktop controllers inject
// these; CLI, serve, and Remote Workbench never set HostTools.
func (a *App) browserHostToolsForTab(tabID string) []tool.HostTool {
	if tabID == "" || a.browser == nil {
		return nil
	}
	read := &browserReadTool{ownerID: func() string { return tabID }, app: a}
	act := &browserActTool{ownerID: func() string { return tabID }, app: a}
	a.browserToolsEnabled.Store(true)
	return []tool.HostTool{
		{
			Name:              "browser_read",
			Description:       read.Description(),
			Schema:            json.RawMessage(browserReadSchema),
			ReadOnly:          true,
			PlanModeSafe:      true,
			HostMutation:      true,
			Source:            "browser",
			Execute:           read.Execute,
			ExecuteWithImages: read.ExecuteWithImages,
		},
		{
			Name:        "browser_act",
			Description: act.Description(),
			Schema:      json.RawMessage(browserActSchema),
			Source:      "browser",
			Execute:     act.Execute,
		},
	}
}

// browserSnapshotText formats a snapshot result for the model.
func browserSnapshotText(res *browseripc.TabSnapshotResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "tab %s: %s (title: %s, generation %d)\n", res.TabID, res.URL, res.Title, res.Generation)
	if res.Snapshot.Tree != "" {
		fmt.Fprintf(&sb, "tree:\n%s\n", res.Snapshot.Tree)
	}
	if res.Snapshot.Text != "" {
		fmt.Fprintf(&sb, "text:\n%s\n", res.Snapshot.Text)
	}
	if res.Snapshot.Truncated {
		sb.WriteString("(output truncated)\n")
	}
	return sb.String()
}

// browserToolError maps wire/coordinator errors to actionable model-visible
// text. Page content never enters these messages.
func browserToolError(err error) error {
	var codeErr *browserCodeError
	if errors.As(err, &codeErr) {
		switch codeErr.code {
		case browseripc.CodeComponentMissing:
			return errors.New("the built-in browser component is not installed. Open any chat link once to install it, or use the settings repair entry. Until then, use web_fetch or open links in the system browser.")
		case browseripc.CodeNotReady:
			return errors.New("the built-in browser is starting; retry the call in a moment")
		case browseripc.CodeCrashed:
			return errors.New("the built-in browser crashed and is restarting; retry the call")
		case browseripc.CodeTabBusy:
			return errors.New("the tab is busy with another navigation; retry after it settles")
		case browseripc.CodeStaleRef:
			return errors.New("the page changed since the snapshot; take a fresh snapshot and retry")
		case browseripc.CodeUserTakeoverReq:
			return errors.New("this action needs the user to complete it in the browser window; wait for the user")
		case browseripc.CodeUserTookControl:
			return errors.New("the user took control of the tab; the action was cancelled")
		case browseripc.CodeTabNotFound:
			return errors.New("the tab is no longer open; use browser_read list_tabs or open a new one")
		case browseripc.CodeTimeout:
			return errors.New("the browser did not answer in time; retry the call")
		case browseripc.CodeUnsupported:
			return errors.New("this browser capability is not implemented in the installed component yet; use web_fetch or the system browser instead")
		case browseripc.CodePermissionDenied:
			return errors.New("the site or action was not permitted")
		case browseripc.CodeInvalidParams:
			return errors.New("invalid browser call parameters")
		}
	}
	if errors.Is(err, ErrBrowserComponentMissing) {
		return errors.New("the built-in browser component is not installed. Open any chat link once to install it, or use the settings repair entry. Until then, use web_fetch or open links in the system browser.")
	}
	if errors.Is(err, ErrBrowserDisabled) {
		return errors.New("the built-in browser is disabled for this session after repeated failures; use the settings recovery entry to re-enable it")
	}
	if errors.Is(err, ErrBrowserNotReady) {
		if errors.Is(err, ErrBrowserComponentMissing) {
			return errors.New("the built-in browser component is not installed. Open any chat link once to install it, or use the settings repair entry. Until then, use web_fetch or open links in the system browser.")
		}
		return errors.New("the built-in browser is not ready; retry the call")
	}
	return err
}
