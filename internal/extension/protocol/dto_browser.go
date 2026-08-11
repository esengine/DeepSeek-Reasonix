package protocol

import (
	"fmt"
	"net/url"
	"strings"
)

// Browser DTOs: Extension → Host restricted browser surface for plugins that
// declare a non-optional requirement on reasonix/browser/companion. Requests
// never carry ownerId, sessionId, runtime generation, or raw CDP parameters;
// those are bound by the host connection.

// BrowserTabDisposition controls whether an opened tab is selected.
type BrowserTabDisposition string

const (
	BrowserDispositionForeground BrowserTabDisposition = "foreground"
	BrowserDispositionBackground BrowserTabDisposition = "background"
)

// BrowserWaitUntil is the page readiness condition for host/browser/tab/wait.
type BrowserWaitUntil string

const (
	BrowserWaitLoad             BrowserWaitUntil = "load"
	BrowserWaitNetworkIdle      BrowserWaitUntil = "network_idle"
	BrowserWaitDOMContentLoaded BrowserWaitUntil = "dom_content_loaded"
	BrowserWaitNavigation       BrowserWaitUntil = "navigation"
)

// BrowserActAction is one restricted input primitive. There is no evaluate /
// screenshot / cookie / download surface.
type BrowserActAction string

const (
	BrowserActClick  BrowserActAction = "click"
	BrowserActHover  BrowserActAction = "hover"
	BrowserActScroll BrowserActAction = "scroll"
	BrowserActType   BrowserActAction = "type"
	BrowserActPress  BrowserActAction = "press"
	BrowserActSelect BrowserActAction = "select"
)

// BrowserTab is the opaque tab identity returned by browser RPCs.
type BrowserTab struct {
	TabID      string `json:"tabId" validate:"nonempty"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Active     bool   `json:"active"`
	Generation uint64 `json:"generation"`
}

// BrowserTabListParams is an empty params object for host/browser/tab/list.
type BrowserTabListParams struct{}

// BrowserTabListResult enumerates the current chat's tabs. Tabs is never null.
type BrowserTabListResult struct {
	Tabs []BrowserTab `json:"tabs"`
}

// BrowserTabOpenParams opens one absolute http(s) URL in a managed tab.
type BrowserTabOpenParams struct {
	URL         string                `json:"url" validate:"nonempty"`
	Disposition BrowserTabDisposition `json:"disposition"`
}

// BrowserTabOpenResult returns the opened tab.
type BrowserTabOpenResult struct {
	Tab BrowserTab `json:"tab"`
}

// BrowserTabSnapshotParams captures a compact accessibility-tree view.
type BrowserTabSnapshotParams struct {
	TabID    string `json:"tabId" validate:"nonempty"`
	MaxChars *int   `json:"maxChars,omitempty" validate:"min=1"`
}

// BrowserTabSnapshotResult is the restricted snapshot payload. It never carries
// HTML, cookies, storage, headers, or screenshots.
type BrowserTabSnapshotResult struct {
	Tab      BrowserTab `json:"tab"`
	Origin   string     `json:"origin"`
	Snapshot string     `json:"snapshot"`
}

// BrowserTabWaitParams waits for a page readiness condition.
type BrowserTabWaitParams struct {
	TabID         string           `json:"tabId" validate:"nonempty"`
	WaitUntil     BrowserWaitUntil `json:"waitUntil"`
	TimeoutMillis *int             `json:"timeoutMillis,omitempty" validate:"min=1,max=30000"`
}

// BrowserTabWaitResult returns the tab after the wait condition.
type BrowserTabWaitResult struct {
	Tab BrowserTab `json:"tab"`
}

// BrowserTabActParams performs one restricted input action.
type BrowserTabActParams struct {
	TabID          string           `json:"tabId" validate:"nonempty"`
	ExpectedOrigin string           `json:"expectedOrigin" validate:"nonempty"`
	Action         BrowserActAction `json:"action"`
	Ref            string           `json:"ref,omitempty"`
	Text           string           `json:"text,omitempty"`
	Key            string           `json:"key,omitempty"`
	Value          string           `json:"value,omitempty"`
	Delta          *int             `json:"delta,omitempty"`
}

// BrowserTabActResult returns the tab after the action completes.
type BrowserTabActResult struct {
	Tab BrowserTab `json:"tab"`
}

// Validate enforces open URL and disposition rules.
func (p BrowserTabOpenParams) Validate() error {
	if err := validateAbsoluteHTTPS(p.URL); err != nil {
		return validationError("params.url " + err.Error())
	}
	switch p.Disposition {
	case BrowserDispositionForeground, BrowserDispositionBackground:
		return nil
	default:
		return validationError(fmt.Sprintf("params.disposition has invalid value %q", p.Disposition))
	}
}

// Validate enforces wait condition and timeout bounds (when present).
func (p BrowserTabWaitParams) Validate() error {
	switch p.WaitUntil {
	case BrowserWaitLoad, BrowserWaitNetworkIdle, BrowserWaitDOMContentLoaded, BrowserWaitNavigation:
	default:
		return validationError(fmt.Sprintf("params.waitUntil has invalid value %q", p.WaitUntil))
	}
	if p.TimeoutMillis != nil {
		if *p.TimeoutMillis < 1 || *p.TimeoutMillis > 30000 {
			return validationError("params.timeoutMillis must be between 1 and 30000")
		}
	}
	return nil
}

// Validate enforces action-specific required fields and origin shape.
func (p BrowserTabActParams) Validate() error {
	if err := validateHTTPOrigin(p.ExpectedOrigin); err != nil {
		return validationError("params.expectedOrigin " + err.Error())
	}
	switch p.Action {
	case BrowserActClick, BrowserActHover, BrowserActType, BrowserActSelect:
		if strings.TrimSpace(p.Ref) == "" {
			return validationError(fmt.Sprintf("params.ref is required for action %q", p.Action))
		}
	case BrowserActScroll:
		if p.Delta == nil || *p.Delta == 0 {
			return validationError("params.delta must be a non-zero integer for action \"scroll\"")
		}
	case BrowserActPress:
		if !isAllowedBrowserKey(p.Key) {
			return validationError(fmt.Sprintf("params.key has invalid value %q", p.Key))
		}
	default:
		return validationError(fmt.Sprintf("params.action has invalid value %q", p.Action))
	}
	return nil
}

func validateAbsoluteHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("must be an absolute http(s) URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("must be an absolute http(s) URL")
	}
	if u.User != nil {
		return fmt.Errorf("must not include userinfo")
	}
	return nil
}

func validateHTTPOrigin(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("must be a scheme://host origin")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("must be an http(s) origin")
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("must not include a path")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("must not include query, fragment, or userinfo")
	}
	return nil
}

// allowedBrowserKeys is the press whitelist. Keep this closed: arbitrary key
// sequences would expand the attack surface without product need.
var allowedBrowserKeys = map[string]struct{}{
	"Enter": {}, "Tab": {}, "Escape": {}, "Backspace": {}, "Delete": {},
	"ArrowUp": {}, "ArrowDown": {}, "ArrowLeft": {}, "ArrowRight": {},
	"Home": {}, "End": {}, "PageUp": {}, "PageDown": {}, " ": {},
}

func isAllowedBrowserKey(key string) bool {
	if key == "" {
		return false
	}
	if _, ok := allowedBrowserKeys[key]; ok {
		return true
	}
	// Single printable ASCII character (letters, digits, punctuation).
	if len(key) == 1 {
		r := key[0]
		return r >= 0x20 && r <= 0x7e
	}
	return false
}
