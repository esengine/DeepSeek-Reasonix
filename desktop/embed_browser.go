package main

import (
	"errors"
	"net/url"
	"strings"
	"sync"
)

var errInvalidEmbedURL = errors.New("仅允许 http(s) URL")

// EmbedBrowserBounds is the panel viewport in window-client DIP coordinates.
// X/Y are relative to the Wails webview (getBoundingClientRect); ScreenX/Y are
// absolute hints kept for compatibility.
type EmbedBrowserBounds struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	ScreenX float64 `json:"screenX"`
	ScreenY float64 `json:"screenY"`
}

// EmbedBrowserState is pushed to the frontend after navigation changes.
type EmbedBrowserState struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	Loading      bool   `json:"loading"`
	Engine       string `json:"engine"`
}

// EmbedBrowserPick is emitted when the user clicks an element in pick mode.
type EmbedBrowserPick struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Selector string  `json:"selector"`
	TagName  string  `json:"tagName"`
	Text     string  `json:"text"`
}

var (
	embedMu  sync.Mutex
	embedApp *App
)

func setEmbedBrowserApp(a *App) {
	embedMu.Lock()
	embedApp = a
	embedMu.Unlock()
}

func (a *App) destroyEmbedBrowser() {
	platformEmbedDestroy()
}

// allowEmbedBrowserURL rejects non-http(s) schemes before they reach the native engine.
func allowEmbedBrowserURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errInvalidEmbedURL
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errInvalidEmbedURL
	}
	return u.String(), nil
}

// EmbedBrowserAvailable reports whether a native embedded WebView can be shown.
func (a *App) EmbedBrowserAvailable() bool {
	return platformEmbedAvailable()
}

func (a *App) EmbedBrowserShow() {
	if err := platformEmbedShow(); err != nil {
		emitEmbedBrowserError(err.Error())
	}
}

func (a *App) EmbedBrowserHide() {
	platformEmbedHide()
}

func (a *App) EmbedBrowserDestroy() {
	platformEmbedDestroy()
}

func (a *App) EmbedBrowserSetBounds(bounds EmbedBrowserBounds) {
	if bounds.Width < 1 || bounds.Height < 1 {
		return
	}
	platformEmbedSetBounds(bounds)
}

func (a *App) EmbedBrowserNavigate(raw string) {
	cleaned, err := allowEmbedBrowserURL(raw)
	if err != nil {
		emitEmbedBrowserError(err.Error())
		return
	}
	if cleaned == "" {
		return
	}
	if err := platformEmbedNavigate(cleaned); err != nil {
		emitEmbedBrowserError(err.Error())
	}
}

func (a *App) EmbedBrowserReload() {
	platformEmbedReload()
}

func (a *App) EmbedBrowserGoBack() {
	platformEmbedGoBack()
}

func (a *App) EmbedBrowserGoForward() {
	platformEmbedGoForward()
}

func (a *App) EmbedBrowserSetZoom(factor float64) {
	if factor < 0.5 {
		factor = 0.5
	}
	if factor > 2 {
		factor = 2
	}
	platformEmbedSetZoom(factor)
}

// EmbedBrowserSnapshotPNG returns a PNG data URL of the current page.
func (a *App) EmbedBrowserSnapshotPNG() (string, error) {
	return platformEmbedSnapshotPNG()
}

// EmbedBrowserSetPickMode enables or disables in-page element picking (Codex-style).
// accent / accentFg are optional CSS colors from the host theme.
func (a *App) EmbedBrowserSetPickMode(enabled bool, accent string, accentFg string) {
	platformEmbedSetPickMode(enabled, accent, accentFg)
}

func emitEmbedBrowserError(msg string) {
	embedMu.Lock()
	a := embedApp
	embedMu.Unlock()
	if a == nil || a.ctx == nil {
		return
	}
	a.runtimeEvents.Emit(a.ctx, "embed-browser:error", msg)
}
