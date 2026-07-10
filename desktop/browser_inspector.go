package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/overlay"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const browserSelectionEvent = "browser:selection"

var browserEditableStyles = map[string]struct{}{
	"align-items": {}, "background-color": {}, "border": {}, "border-color": {},
	"border-radius": {}, "border-style": {}, "border-width": {}, "color": {},
	"display": {}, "flex-direction": {}, "font-family": {}, "font-size": {},
	"font-weight": {}, "gap": {}, "grid-template-columns": {}, "height": {},
	"justify-content": {}, "line-height": {}, "margin": {}, "margin-bottom": {},
	"margin-left": {}, "margin-right": {}, "margin-top": {}, "opacity": {},
	"padding": {}, "padding-bottom": {}, "padding-left": {}, "padding-right": {},
	"padding-top": {}, "width": {},
}

var browserComputedStyleNames = []string{
	"color", "background-color", "opacity", "font-family", "font-size", "font-weight",
	"line-height", "margin-top", "margin-right", "margin-bottom", "margin-left",
	"padding-top", "padding-right", "padding-bottom", "padding-left", "gap", "width",
	"height", "border", "border-radius", "display", "flex-direction", "align-items",
	"justify-content", "grid-template-columns",
}

type BrowserElementBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type BrowserElementView struct {
	TabID          string            `json:"tabId"`
	PageID         string            `json:"pageId"`
	FrameID        string            `json:"frameId,omitempty"`
	BackendNodeID  int64             `json:"backendNodeId"`
	URL            string            `json:"url"`
	Title          string            `json:"title,omitempty"`
	Tag            string            `json:"tag"`
	Selector       string            `json:"selector"`
	AccessibleName string            `json:"accessibleName,omitempty"`
	Text           string            `json:"text,omitempty"`
	OuterHTML      string            `json:"outerHTML,omitempty"`
	Box            BrowserElementBox `json:"box"`
	ComputedStyles map[string]string `json:"computedStyles"`
	StyleOverrides map[string]string `json:"styleOverrides,omitempty"`
}

type BrowserAnnotationCapture struct {
	ScreenshotData        string `json:"screenshotData"`
	ElementScreenshotData string `json:"elementScreenshotData,omitempty"`
}

type browserSelectionPayload struct {
	TabID     string              `json:"tabId"`
	PageID    string              `json:"pageId"`
	Sequence  uint64              `json:"sequence"`
	Selection *BrowserElementView `json:"selection,omitempty"`
}

type browserInspectorState struct {
	backendNodeID cdp.BackendNodeID
	frameID       cdp.FrameID
	selection     BrowserElementView
	overrides     map[string]string
	sequence      uint64
}

func (a *App) BrowserInspectorHover(tabID, pageID string, x, y float64) error {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return err
	}
	return session.inspectorHover(x, y)
}

func (a *App) BrowserInspectorSelect(tabID, pageID string, x, y float64) (BrowserElementView, error) {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return BrowserElementView{}, err
	}
	return session.inspectorSelect(x, y)
}

func (a *App) BrowserApplyStyles(tabID, pageID string, styles map[string]string) (BrowserElementView, error) {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return BrowserElementView{}, err
	}
	return session.applyInspectorStyles(styles)
}

func (a *App) BrowserCaptureAnnotation(tabID, pageID string) (BrowserAnnotationCapture, error) {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return BrowserAnnotationCapture{}, err
	}
	return session.captureInspectorScreenshots()
}

func (a *App) BrowserInspectorClear(tabID, pageID string) error {
	session, err := a.browserManager().session(strings.TrimSpace(tabID), strings.TrimSpace(pageID))
	if err != nil {
		return err
	}
	return session.clearInspector(true)
}

func (s *browserSession) inspectorHover(x, y float64) error {
	return s.runCommand(func(ctx context.Context) error {
		x, y := s.clampInspectorPoint(x, y)
		backendNodeID, _, err := browserNodeAt(ctx, x, y)
		if err != nil {
			return err
		}
		return browserHighlightNode(ctx, backendNodeID, false)
	})
}

func (s *browserSession) inspectorSelect(x, y float64) (BrowserElementView, error) {
	var selection BrowserElementView
	var backendNodeID cdp.BackendNodeID
	var frameID cdp.FrameID
	err := s.runCommand(func(ctx context.Context) error {
		var err error
		x, y := s.clampInspectorPoint(x, y)
		backendNodeID, frameID, err = browserNodeAt(ctx, x, y)
		if err != nil {
			return err
		}
		if err := browserHighlightNode(ctx, backendNodeID, true); err != nil {
			return err
		}
		selection, err = browserDescribeElement(ctx, backendNodeID)
		return err
	})
	if err != nil {
		return BrowserElementView{}, err
	}
	state := s.view()
	selection.TabID = s.tabID
	selection.PageID = s.pageID()
	selection.FrameID = string(frameID)
	selection.BackendNodeID = int64(backendNodeID)
	selection.URL = state.URL
	selection.Title = state.Title

	s.inspectorMu.Lock()
	s.inspectorSequence++
	sequence := s.inspectorSequence
	s.inspector = &browserInspectorState{
		backendNodeID: backendNodeID,
		frameID:       frameID,
		selection:     selection,
		overrides:     map[string]string{},
		sequence:      sequence,
	}
	s.inspectorMu.Unlock()
	s.emitSelection(sequence, &selection)
	return selection, nil
}

func (s *browserSession) applyInspectorStyles(styles map[string]string) (BrowserElementView, error) {
	normalized, err := normalizeBrowserStyleOverrides(styles)
	if err != nil {
		return BrowserElementView{}, err
	}
	s.inspectorMu.Lock()
	if s.inspector == nil {
		s.inspectorMu.Unlock()
		return BrowserElementView{}, errors.New("no browser element is selected")
	}
	selector := s.inspector.selection.Selector
	s.inspectorMu.Unlock()

	if err := s.runCommand(func(ctx context.Context) error {
		return applyBrowserStyleSheet(ctx, selector, normalized)
	}); err != nil {
		return BrowserElementView{}, err
	}

	s.inspectorMu.Lock()
	if s.inspector == nil || s.inspector.selection.Selector != selector {
		s.inspectorMu.Unlock()
		return BrowserElementView{}, errors.New("browser selection changed")
	}
	s.inspector.overrides = normalized
	s.inspector.selection.StyleOverrides = cloneBrowserStyles(normalized)
	s.inspectorSequence++
	sequence := s.inspectorSequence
	s.inspector.sequence = sequence
	selection := s.inspector.selection
	s.inspectorMu.Unlock()
	s.emitSelection(sequence, &selection)
	return selection, nil
}

func (s *browserSession) captureInspectorScreenshots() (BrowserAnnotationCapture, error) {
	s.inspectorMu.Lock()
	if s.inspector == nil {
		s.inspectorMu.Unlock()
		return BrowserAnnotationCapture{}, errors.New("no browser element is selected")
	}
	box := s.inspector.selection.Box
	s.inspectorMu.Unlock()

	state := s.view()
	x := math.Max(0, box.X)
	y := math.Max(0, box.Y)
	width := math.Min(box.Width, float64(state.Width)-x)
	height := math.Min(box.Height, float64(state.Height)-y)
	if width <= 0 || height <= 0 {
		return BrowserAnnotationCapture{}, errors.New("selected browser element is outside the viewport")
	}

	var screenshot []byte
	var elementScreenshot []byte
	err := s.runCommand(func(ctx context.Context) error {
		var err error
		screenshot, err = page.CaptureScreenshot().
			WithFormat(page.CaptureScreenshotFormatJpeg).
			WithQuality(88).
			WithFromSurface(true).
			WithCaptureBeyondViewport(false).
			Do(ctx)
		if err != nil {
			return err
		}
		elementScreenshot, err = page.CaptureScreenshot().
			WithFormat(page.CaptureScreenshotFormatJpeg).
			WithQuality(92).
			WithFromSurface(true).
			WithCaptureBeyondViewport(false).
			WithClip(&page.Viewport{X: x, Y: y, Width: width, Height: height, Scale: 1}).
			Do(ctx)
		return err
	})
	if err != nil {
		return BrowserAnnotationCapture{}, fmt.Errorf("capture browser annotation: %w", err)
	}
	return BrowserAnnotationCapture{
		ScreenshotData:        base64.StdEncoding.EncodeToString(screenshot),
		ElementScreenshotData: base64.StdEncoding.EncodeToString(elementScreenshot),
	}, nil
}

func (s *browserSession) clearInspector(emit bool) error {
	s.inspectorMu.Lock()
	s.inspector = nil
	s.inspectorSequence++
	sequence := s.inspectorSequence
	s.inspectorMu.Unlock()
	err := s.runCommand(func(ctx context.Context) error {
		if err := overlay.HideHighlight().Do(ctx); err != nil {
			return err
		}
		return removeBrowserStyleSheet(ctx)
	})
	if emit {
		s.emitSelection(sequence, nil)
	}
	return err
}

func (s *browserSession) resetInspector() {
	s.inspectorMu.Lock()
	hadInspector := s.inspector != nil
	s.inspector = nil
	if hadInspector {
		s.inspectorSequence++
	}
	sequence := s.inspectorSequence
	s.inspectorMu.Unlock()
	if hadInspector {
		s.emitSelection(sequence, nil)
	}
}

func (s *browserSession) clearInspectorOnNavigation() {
	s.inspectorMu.Lock()
	hasInspector := s.inspector != nil
	s.inspectorMu.Unlock()
	if hasInspector {
		_ = s.clearInspector(true)
	}
}

func (s *browserSession) emitSelection(sequence uint64, selection *BrowserElementView) {
	s.manager.app.runtimeEvents.Emit(s.manager.app.bootContext(), browserSelectionEvent, browserSelectionPayload{
		TabID:     s.tabID,
		PageID:    s.pageID(),
		Sequence:  sequence,
		Selection: selection,
	})
}

func (s *browserSession) clampInspectorPoint(x, y float64) (int64, int64) {
	x, y = s.clampPoint(x, y)
	return int64(math.Round(x)), int64(math.Round(y))
}

func browserNodeAt(ctx context.Context, x, y int64) (cdp.BackendNodeID, cdp.FrameID, error) {
	if err := dom.Enable().Do(ctx); err != nil {
		return 0, "", err
	}
	backendNodeID, frameID, _, err := dom.GetNodeForLocation(x, y).
		WithIncludeUserAgentShadowDOM(true).
		WithIgnorePointerEventsNone(true).
		Do(ctx)
	if err != nil {
		return 0, "", err
	}
	if backendNodeID == 0 {
		return 0, "", errors.New("no DOM element at browser coordinates")
	}
	return backendNodeID, frameID, nil
}

func browserHighlightNode(ctx context.Context, backendNodeID cdp.BackendNodeID, selected bool) error {
	if err := overlay.Enable().Do(ctx); err != nil {
		return err
	}
	config := &overlay.HighlightConfig{
		ShowInfo:              selected,
		ShowStyles:            selected,
		ShowAccessibilityInfo: selected,
		ContentColor:          &cdp.RGBA{R: 80, G: 150, B: 255, A: 0.18},
		PaddingColor:          &cdp.RGBA{R: 90, G: 210, B: 150, A: 0.16},
		BorderColor:           &cdp.RGBA{R: 255, G: 190, B: 70, A: 0.5},
		MarginColor:           &cdp.RGBA{R: 255, G: 120, B: 90, A: 0.16},
		ColorFormat:           overlay.ColorFormatHex,
	}
	return overlay.HighlightNode(config).WithBackendNodeID(backendNodeID).Do(ctx)
}

const browserDescribeElementFunction = `function() {
  const element = this;
  const clip = (value, limit) => String(value || "").replace(/\s+/g, " ").trim().slice(0, limit);
  const selector = (() => {
    if (element.id && document.querySelectorAll("#" + CSS.escape(element.id)).length === 1) {
      return "#" + CSS.escape(element.id);
    }
    const parts = [];
    let node = element;
    while (node && node.nodeType === Node.ELEMENT_NODE && parts.length < 7) {
      let part = node.localName;
      const classes = Array.from(node.classList || []).filter(Boolean).slice(0, 2);
      if (classes.length) part += "." + classes.map((name) => CSS.escape(name)).join(".");
      if (node.parentElement) {
        const siblings = Array.from(node.parentElement.children).filter((child) => child.localName === node.localName);
        if (siblings.length > 1) part += ":nth-of-type(" + (siblings.indexOf(node) + 1) + ")";
      }
      parts.unshift(part);
      const candidate = parts.join(" > ");
      try {
        if (document.querySelectorAll(candidate).length === 1) return candidate;
      } catch (_) {}
      node = node.parentElement;
    }
    return parts.join(" > ");
  })();
  const rect = element.getBoundingClientRect();
  const computed = getComputedStyle(element);
  const names = ` + "__STYLE_NAMES__" + `;
  const computedStyles = {};
  for (const name of names) computedStyles[name] = computed.getPropertyValue(name).trim();
  return {
    tag: element.localName || element.nodeName.toLowerCase(),
    selector,
    accessibleName: clip(element.getAttribute("aria-label") || element.getAttribute("title") || element.getAttribute("alt") || element.innerText, 240),
    text: clip(element.innerText || element.textContent, 320),
    outerHTML: clip(element.outerHTML, 4096),
    box: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
    computedStyles
  };
}`

func browserDescribeElement(ctx context.Context, backendNodeID cdp.BackendNodeID) (BrowserElementView, error) {
	object, err := dom.ResolveNode().WithBackendNodeID(backendNodeID).WithObjectGroup("reasonix-inspector").Do(ctx)
	if err != nil {
		return BrowserElementView{}, err
	}
	if object == nil || object.ObjectID == "" {
		return BrowserElementView{}, errors.New("selected DOM node is not a JavaScript element")
	}
	defer func() { _ = cdpruntime.ReleaseObject(object.ObjectID).Do(ctx) }()
	styleNames, _ := json.Marshal(browserComputedStyleNames)
	declaration := strings.Replace(browserDescribeElementFunction, "__STYLE_NAMES__", string(styleNames), 1)
	result, exception, err := cdpruntime.CallFunctionOn(declaration).
		WithObjectID(object.ObjectID).
		WithReturnByValue(true).
		Do(ctx)
	if err != nil {
		return BrowserElementView{}, err
	}
	if exception != nil {
		return BrowserElementView{}, fmt.Errorf("inspect DOM element: %s", exception.Text)
	}
	if result == nil || len(result.Value) == 0 {
		return BrowserElementView{}, errors.New("browser DOM inspection returned no data")
	}
	var selection BrowserElementView
	if err := json.Unmarshal([]byte(result.Value), &selection); err != nil {
		return BrowserElementView{}, fmt.Errorf("decode browser DOM inspection: %w", err)
	}
	if selection.Selector == "" {
		return BrowserElementView{}, errors.New("could not build a stable selector for browser element")
	}
	return selection, nil
}

func normalizeBrowserStyleOverrides(styles map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(styles))
	for rawName, rawValue := range styles {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if _, ok := browserEditableStyles[name]; !ok {
			return nil, fmt.Errorf("browser style %q is not editable", rawName)
		}
		value := strings.TrimSpace(rawValue)
		if value == "" {
			continue
		}
		if utf8.RuneCountInString(value) > 256 {
			return nil, fmt.Errorf("browser style %q is too long", name)
		}
		if strings.ContainsAny(value, ";{}<>\r\n") {
			return nil, fmt.Errorf("browser style %q contains unsupported characters", name)
		}
		out[name] = value
	}
	return out, nil
}

func applyBrowserStyleSheet(ctx context.Context, selector string, styles map[string]string) error {
	if strings.ContainsAny(selector, "{}<>\r\n") {
		return errors.New("browser selector contains unsupported characters")
	}
	payload, err := json.Marshal(struct {
		Selector string            `json:"selector"`
		Styles   map[string]string `json:"styles"`
	}{Selector: selector, Styles: styles})
	if err != nil {
		return err
	}
	expression := `(() => {
  const payload = ` + string(payload) + `;
  const id = "__reasonix_inspector_styles__";
  let style = document.getElementById(id);
  if (!Object.keys(payload.styles).length) {
    if (style) style.remove();
    return true;
  }
  if (!style) {
    style = document.createElement("style");
    style.id = id;
    (document.head || document.documentElement).appendChild(style);
  }
  const body = Object.entries(payload.styles).map(([name, value]) => name + ":" + value + " !important").join(";");
  style.textContent = payload.selector + "{" + body + "}";
  return true;
})()`
	var applied bool
	return chromedp.Evaluate(expression, &applied).Do(ctx)
}

func removeBrowserStyleSheet(ctx context.Context) error {
	var removed bool
	return chromedp.Evaluate(`(() => { const node = document.getElementById("__reasonix_inspector_styles__"); if (node) node.remove(); return true; })()`, &removed).Do(ctx)
}

func cloneBrowserStyles(styles map[string]string) map[string]string {
	if len(styles) == 0 {
		return nil
	}
	keys := make([]string, 0, len(styles))
	for key := range styles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(styles))
	for _, key := range keys {
		out[key] = styles[key]
	}
	return out
}
