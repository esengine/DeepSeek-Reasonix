package browseripc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// This file mirrors schema.json method-by-method. TestSchemaParity walks the
// schema and proves every method, field, and limit has a Go counterpart, so
// the hand-written types cannot drift from the canonical document.

// ---- hello ----

// HelloParams is sent by the host after spawning the companion. The companion
// must not open its window or accept other methods before it has answered.
type HelloParams struct {
	HostName    string `json:"hostName"`
	HostVersion string `json:"hostVersion"`
}

// Capabilities is the protocol capability declaration returned in the hello
// handshake. It is metadata only: method dispatch is still governed by the
// canonical schema.
type Capabilities struct {
	MaxProtocolVersion int      `json:"maxProtocolVersion"`
	Methods            []string `json:"methods"`
	Events             []string `json:"events"`
}

// HelloResult is the companion's handshake answer.
type HelloResult struct {
	ProtocolVersion  int          `json:"protocolVersion"`
	ComponentVersion string       `json:"componentVersion"`
	ElectronVersion  string       `json:"electronVersion"`
	ChromiumVersion  string       `json:"chromiumVersion"`
	PID              int          `json:"pid"`
	Capabilities     Capabilities `json:"capabilities"`
}

// ---- request.cancel ----

// CancelParams aborts a pending request by its requestId.
type CancelParams struct {
	RequestID string `json:"requestId"`
}

// ---- owner scoped helpers ----

// OwnerParams carries an ownerId for window/owner methods.
type OwnerParams struct {
	OwnerID string `json:"ownerId"`
}

// ---- tabs ----

// Disposition selects where a new tab opens.
type Disposition string

const (
	DispositionForeground Disposition = "foreground"
	DispositionBackground Disposition = "background"
)

// TabOpenParams opens a new tab for the owner. fromAgent marks agent-originated
// opens so the companion can apply agent-specific policy (site admission).
type TabOpenParams struct {
	OwnerID     string      `json:"ownerId"`
	URL         string      `json:"url"`
	Disposition Disposition `json:"disposition"`
	FromAgent   bool        `json:"fromAgent,omitempty"`
}

// TabInfo is the stable per-tab description returned by list and open.
type TabInfo struct {
	TabID      string `json:"tabId"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Active     bool   `json:"active"`
	Generation int64  `json:"generation"`
}

// TabListResult lists the owner's tabs in stable tab order.
type TabListResult struct {
	Tabs []TabInfo `json:"tabs"`
}

// TabRefParams addresses an existing tab.
type TabRefParams struct {
	OwnerID string `json:"ownerId"`
	TabID   string `json:"tabId"`
}

// TabNavigateParams navigates an existing tab.
type TabNavigateParams struct {
	OwnerID string `json:"ownerId"`
	TabID   string `json:"tabId"`
	URL     string `json:"url"`
}

// TabNavigateResult reports the post-navigation tab state.
type TabNavigateResult struct {
	TabID      string `json:"tabId"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Generation int64  `json:"generation"`
	Active     bool   `json:"active"`
}

// TabSnapshotParams requests the compacted accessibility tree of a tab.
type TabSnapshotParams struct {
	OwnerID  string `json:"ownerId"`
	TabID    string `json:"tabId"`
	MaxChars *int   `json:"maxChars,omitempty"`
}

// SnapshotData is the compacted page view: a semantic tree with opaque refs
// plus plain text. Page content is untrusted external data and is bounded by
// MaxTextChars.
type SnapshotData struct {
	Tree      string `json:"tree"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// TabSnapshotResult carries the snapshot plus the tab state it was taken on.
type TabSnapshotResult struct {
	TabID      string       `json:"tabId"`
	URL        string       `json:"url"`
	Title      string       `json:"title"`
	Generation int64        `json:"generation"`
	Snapshot   SnapshotData `json:"snapshot"`
}

// TabScreenshotParams captures a tab screenshot.
type TabScreenshotParams struct {
	OwnerID string `json:"ownerId"`
	TabID   string `json:"tabId"`
}

// TabScreenshotResult carries the screenshot as a data URL (the host forwards
// it through the ImageTool channel, never into text results).
type TabScreenshotResult struct {
	TabID        string `json:"tabId"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	Generation   int64  `json:"generation"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ImageDataURL string `json:"imageDataUrl"`
}

// WaitUntil selects the navigation state tab.wait blocks for.
type WaitUntil string

const (
	WaitLoad        WaitUntil = "load"
	WaitNetworkIdle WaitUntil = "network_idle"
	WaitDOMContent  WaitUntil = "dom_content_loaded"
	WaitNavigation  WaitUntil = "navigation"
)

// TabWaitParams waits for a navigation/lifecycle state of the tab.
type TabWaitParams struct {
	OwnerID   string    `json:"ownerId"`
	TabID     string    `json:"tabId"`
	WaitUntil WaitUntil `json:"waitUntil"`
	TimeoutMs *int      `json:"timeoutMs,omitempty"`
}

// TabWaitResult reports the tab state after the wait.
type TabWaitResult struct {
	TabID      string `json:"tabId"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Generation int64  `json:"generation"`
}

// ActAction is a restricted agent input action. The companion executes only
// these primitives through the Input domain; there is no arbitrary JavaScript
// path.
type ActAction string

const (
	ActClick  ActAction = "click"
	ActHover  ActAction = "hover"
	ActScroll ActAction = "scroll"
	ActType   ActAction = "type"
	ActPress  ActAction = "press"
	ActSelect ActAction = "select"
)

// TabActParams performs one restricted input action. expectedOrigin must match
// the tab's current origin exactly; ref addresses an interactive node from a
// prior snapshot and is generation-bound (stale_ref after navigation).
type TabActParams struct {
	OwnerID        string    `json:"ownerId"`
	TabID          string    `json:"tabId"`
	ExpectedOrigin string    `json:"expectedOrigin,omitempty"`
	Action         ActAction `json:"action"`
	Ref            string    `json:"ref,omitempty"`
	Text           string    `json:"text,omitempty"`
	Key            string    `json:"key,omitempty"`
	Value          string    `json:"value,omitempty"`
	Delta          *int      `json:"delta,omitempty"`
}

// TabActResult mirrors TabWaitResult.
type TabActResult = TabWaitResult

// ---- data ----

// ClearScope selects a browsing data category. "all" clears every category.
type ClearScope string

const (
	ClearHistory   ClearScope = "history"
	ClearCookies   ClearScope = "cookies"
	ClearCache     ClearScope = "cache"
	ClearDownloads ClearScope = "downloads"
	ClearAll       ClearScope = "all"
)

// DataClearParams requests profile data clearing inside the companion's
// isolated Chromium session.
type DataClearParams struct {
	Scopes []ClearScope `json:"scopes"`
}

// DataClearResult reports which scopes were actually cleared.
type DataClearResult struct {
	Cleared []string `json:"cleared"`
}

// ---- permissions ----

// SiteGrant is one agent site-admission grant. Origins are normalized as
// scheme://punycode-host:effective-port with no wildcards.
type SiteGrant struct {
	Origin       string   `json:"origin"`
	GrantedAt    string   `json:"grantedAt"`
	Capabilities []string `json:"capabilities"`
}

// PermissionsListResult returns all recorded agent site grants.
type PermissionsListResult struct {
	Grants []SiteGrant `json:"grants"`
}

// PermissionsRevokeParams revokes one exact normalized origin.
type PermissionsRevokeParams struct {
	Origin string `json:"origin"`
}

// ---- events ----

// TabChangedEventData is emitted when a tab's URL/title/active state changes.
type TabChangedEventData struct {
	OwnerID    string `json:"ownerId"`
	TabID      string `json:"tabId"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	Active     bool   `json:"active"`
	Generation int64  `json:"generation"`
}

// NavigationEventState is the lifecycle phase of a navigation.
type NavigationEventState string

const (
	NavStarted   NavigationEventState = "started"
	NavCommitted NavigationEventState = "committed"
	NavFailed    NavigationEventState = "failed"
)

// NavigationEventData is emitted around navigations.
type NavigationEventData struct {
	OwnerID string               `json:"ownerId"`
	TabID   string               `json:"tabId"`
	URL     string               `json:"url"`
	Title   string               `json:"title"`
	State   NavigationEventState `json:"state"`
}

// DownloadEventState is the lifecycle phase of a download.
type DownloadEventState string

const (
	DownloadStarted   DownloadEventState = "started"
	DownloadCompleted DownloadEventState = "completed"
	DownloadCancelled DownloadEventState = "cancelled"
	DownloadFailed    DownloadEventState = "failed"
)

// DownloadEventData is emitted for user-confirmed downloads. Filenames are
// host-visual only and never enter agent tool results or telemetry.
type DownloadEventData struct {
	OwnerID  string             `json:"ownerId"`
	TabID    string             `json:"tabId"`
	Filename string             `json:"filename"`
	MIME     string             `json:"mime"`
	State    DownloadEventState `json:"state"`
}

// PermissionRequestEventData notifies the host of a human-only Chromium
// permission prompt (camera, microphone, location, ...). The agent can never
// approve it.
type PermissionRequestEventData struct {
	OwnerID    string `json:"ownerId"`
	Origin     string `json:"origin"`
	Capability string `json:"capability"`
}

// AgentTakeoverReason explains why the agent lease was revoked.
type AgentTakeoverReason string

const (
	TakeoverUser       AgentTakeoverReason = "user"
	TakeoverPermission AgentTakeoverReason = "permission"
	TakeoverSensitive  AgentTakeoverReason = "sensitive_field"
)

// AgentTakeoverEventData is emitted when the agent lease for a tab is revoked.
type AgentTakeoverEventData struct {
	OwnerID string              `json:"ownerId"`
	TabID   string              `json:"tabId"`
	Reason  AgentTakeoverReason `json:"reason"`
}

// RendererCrashEventData is emitted when a tab's renderer crashes; only the
// affected tab is rebuilt.
type RendererCrashEventData struct {
	OwnerID string `json:"ownerId"`
	TabID   string `json:"tabId"`
}

// CDPDetachEventData is emitted when the CDP controller detaches from a tab.
type CDPDetachEventData struct {
	OwnerID string `json:"ownerId"`
	TabID   string `json:"tabId"`
	Reason  string `json:"reason"`
}

// ---- validation ----

// MethodNames lists every request method in canonical (schema) order.
var MethodNames = []string{
	"hello",
	"request.cancel",
	"window.open",
	"window.focus",
	"window.close",
	"owner.activate",
	"owner.remove",
	"tab.open",
	"tab.list",
	"tab.activate",
	"tab.close",
	"tab.navigate",
	"tab.snapshot",
	"tab.screenshot",
	"tab.wait",
	"tab.act",
	"data.clear",
	"permissions.list",
	"permissions.revoke",
}

// EventNames lists every companion-to-host event in canonical order.
var EventNames = []string{
	"tab.changed",
	"navigation",
	"download",
	"permission.request",
	"agent.takeover",
	"renderer.crash",
	"cdp.detach",
}

// ErrorCodes lists every protocol error code in canonical order.
var ErrorCodes = []ErrorCode{
	CodeCancelled,
	CodeComponentMissing,
	CodeCrashed,
	CodeFrameTooLarge,
	CodeInternal,
	CodeInvalidParams,
	CodeNotReady,
	CodeOwnerNotFound,
	CodePermissionDenied,
	CodeProtocolMismatch,
	CodeStaleRef,
	CodeTabBusy,
	CodeTabNotFound,
	CodeTimeout,
	CodeUnknownMethod,
	CodeUnsupported,
	CodeUserTakeoverReq,
	CodeUserTookControl,
}

var knownMethods = func() map[string]bool {
	m := make(map[string]bool, len(MethodNames))
	for _, name := range MethodNames {
		m[name] = true
	}
	return m
}()

var knownEvents = func() map[string]bool {
	m := make(map[string]bool, len(EventNames))
	for _, name := range EventNames {
		m[name] = true
	}
	return m
}()

// IsKnownMethod reports whether name is a canonical request method.
func IsKnownMethod(name string) bool { return knownMethods[name] }

// IsKnownEvent reports whether name is a canonical companion event.
func IsKnownEvent(name string) bool { return knownEvents[name] }

// ValidateRequest enforces envelope rules and per-method params shape. It is
// the single validation entry point for both sides of the pipe: the Go host
// validates before sending, the Electron companion before dispatching.
func ValidateRequest(req Request) error {
	if req.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("protocol version %d != %d", req.ProtocolVersion, ProtocolVersion)
	}
	if err := checkToken(req.RequestID, "requestId", MaxRequestIDBytes); err != nil {
		return err
	}
	if err := checkToken(req.OwnerID, "ownerId", MaxOwnerIDBytes); err != nil {
		return err
	}
	if err := checkToken(req.Method, "method", MaxMethodBytes); err != nil {
		return err
	}
	if !knownMethods[req.Method] {
		return fmt.Errorf("unknown method %q", req.Method)
	}
	if req.Params == nil {
		req.Params = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(req.Params))
	dec.DisallowUnknownFields()
	switch req.Method {
	case "hello":
		var p HelloParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("hello params: %w", err)
		}
		if strings.TrimSpace(p.HostName) == "" || strings.TrimSpace(p.HostVersion) == "" {
			return fmt.Errorf("hello params: hostName and hostVersion are required")
		}
	case "request.cancel":
		var p CancelParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("request.cancel params: %w", err)
		}
		if err := checkToken(p.RequestID, "requestId", MaxRequestIDBytes); err != nil {
			return err
		}
	case "window.open", "window.focus", "owner.activate", "owner.remove":
		var p OwnerParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("%s params: %w", req.Method, err)
		}
		if strings.TrimSpace(p.OwnerID) == "" {
			return fmt.Errorf("%s params: ownerId is required", req.Method)
		}
	case "window.close", "permissions.list":
		// Empty params object.
		if err := dec.Decode(&struct{}{}); err != nil {
			return fmt.Errorf("%s params: %w", req.Method, err)
		}
	case "tab.open":
		var p TabOpenParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("tab.open params: %w", err)
		}
		if strings.TrimSpace(p.OwnerID) == "" {
			return fmt.Errorf("tab.open params: ownerId is required")
		}
		if p.Disposition != DispositionForeground && p.Disposition != DispositionBackground {
			return fmt.Errorf("tab.open params: invalid disposition %q", p.Disposition)
		}
		if err := checkURL(p.URL); err != nil {
			return fmt.Errorf("tab.open params: %w", err)
		}
	case "tab.list":
		var p OwnerParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("tab.list params: %w", err)
		}
		if strings.TrimSpace(p.OwnerID) == "" {
			return fmt.Errorf("tab.list params: ownerId is required")
		}
	case "tab.activate", "tab.close":
		var p TabRefParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("%s params: %w", req.Method, err)
		}
		if err := checkOwnerTab(p); err != nil {
			return err
		}
	case "tab.navigate":
		var p TabNavigateParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("tab.navigate params: %w", err)
		}
		if err := checkOwnerTab(p); err != nil {
			return err
		}
		if err := checkURL(p.URL); err != nil {
			return fmt.Errorf("tab.navigate params: %w", err)
		}
	case "tab.snapshot":
		var p TabSnapshotParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("tab.snapshot params: %w", err)
		}
		if err := checkOwnerTab(p); err != nil {
			return err
		}
	case "tab.screenshot":
		var p TabScreenshotParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("tab.screenshot params: %w", err)
		}
		if err := checkOwnerTab(p); err != nil {
			return err
		}
	case "tab.wait":
		var p TabWaitParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("tab.wait params: %w", err)
		}
		if err := checkOwnerTab(p); err != nil {
			return err
		}
		if p.WaitUntil != WaitLoad && p.WaitUntil != WaitNetworkIdle &&
			p.WaitUntil != WaitDOMContent && p.WaitUntil != WaitNavigation {
			return fmt.Errorf("tab.wait params: invalid waitUntil %q", p.WaitUntil)
		}
	case "tab.act":
		var p TabActParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("tab.act params: %w", err)
		}
		if err := checkOwnerTab(p); err != nil {
			return err
		}
		switch p.Action {
		case ActClick, ActHover, ActScroll, ActType, ActPress, ActSelect:
		default:
			return fmt.Errorf("tab.act params: invalid action %q", p.Action)
		}
		if p.Action == ActClick || p.Action == ActHover {
			if strings.TrimSpace(p.Ref) == "" {
				return fmt.Errorf("tab.act params: ref is required for %s", p.Action)
			}
		}
		if p.Action == ActType {
			if len(p.Text) > MaxTextChars {
				return fmt.Errorf("tab.act params: text exceeds %d chars", MaxTextChars)
			}
		}
		if len(p.ExpectedOrigin) > MaxOriginBytes {
			return fmt.Errorf("tab.act params: expectedOrigin too long")
		}
	case "data.clear":
		var p DataClearParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("data.clear params: %w", err)
		}
		if len(p.Scopes) == 0 {
			return fmt.Errorf("data.clear params: scopes is required")
		}
		for _, s := range p.Scopes {
			switch s {
			case ClearHistory, ClearCookies, ClearCache, ClearDownloads, ClearAll:
			default:
				return fmt.Errorf("data.clear params: invalid scope %q", s)
			}
		}
	case "permissions.revoke":
		var p PermissionsRevokeParams
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("permissions.revoke params: %w", err)
		}
		if len(p.Origin) > MaxOriginBytes || strings.TrimSpace(p.Origin) == "" {
			return fmt.Errorf("permissions.revoke params: invalid origin")
		}
	default:
		return fmt.Errorf("unknown method %q", req.Method)
	}
	return nil
}

func checkOwnerTab(p interface {
	GetOwnerID() string
	GetTabID() string
}) error {
	if strings.TrimSpace(p.GetOwnerID()) == "" {
		return fmt.Errorf("params: ownerId is required")
	}
	if strings.TrimSpace(p.GetTabID()) == "" {
		return fmt.Errorf("params: tabId is required")
	}
	return nil
}

func (p TabRefParams) GetOwnerID() string      { return p.OwnerID }
func (p TabRefParams) GetTabID() string        { return p.TabID }
func (p TabNavigateParams) GetOwnerID() string { return p.OwnerID }
func (p TabNavigateParams) GetTabID() string   { return p.TabID }
func (p TabSnapshotParams) GetOwnerID() string { return p.OwnerID }
func (p TabSnapshotParams) GetTabID() string   { return p.TabID }
func (p TabScreenshotParams) GetOwnerID() string {
	return p.OwnerID
}
func (p TabScreenshotParams) GetTabID() string { return p.TabID }
func (p TabWaitParams) GetOwnerID() string     { return p.OwnerID }
func (p TabWaitParams) GetTabID() string       { return p.TabID }
func (p TabActParams) GetOwnerID() string      { return p.OwnerID }
func (p TabActParams) GetTabID() string        { return p.TabID }

// ValidateResponse enforces the response envelope: protocol version, requestId
// bounds, and exactly one of result/error.
func ValidateResponse(resp Response) error {
	if resp.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("protocol version %d != %d", resp.ProtocolVersion, ProtocolVersion)
	}
	if err := checkToken(resp.RequestID, "requestId", MaxRequestIDBytes); err != nil {
		return err
	}
	hasResult := len(resp.Result) > 0
	hasError := resp.Error != nil
	if hasResult == hasError {
		return fmt.Errorf("response must set exactly one of result or error")
	}
	if hasError {
		switch resp.Error.Code {
		case CodeCancelled, CodeComponentMissing, CodeCrashed, CodeFrameTooLarge,
			CodeInternal, CodeInvalidParams, CodeNotReady, CodeOwnerNotFound,
			CodePermissionDenied, CodeProtocolMismatch, CodeStaleRef, CodeTabBusy,
			CodeTabNotFound, CodeTimeout, CodeUnknownMethod, CodeUnsupported,
			CodeUserTakeoverReq, CodeUserTookControl:
		default:
			return fmt.Errorf("unknown error code %q", resp.Error.Code)
		}
	}
	return nil
}

// ValidateEvent enforces the event envelope: protocol version, known event
// name, ownerId bounds, and non-empty data.
func ValidateEvent(ev Event) error {
	if ev.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("protocol version %d != %d", ev.ProtocolVersion, ProtocolVersion)
	}
	if !knownEvents[ev.Event.Name] {
		return fmt.Errorf("unknown event %q", ev.Event.Name)
	}
	if err := checkToken(ev.Event.OwnerID, "ownerId", MaxOwnerIDBytes); err != nil {
		return err
	}
	if len(ev.Event.Data) == 0 {
		return fmt.Errorf("event %q has no data", ev.Event.Name)
	}
	return nil
}

func checkToken(value, name string, max int) error {
	if len(value) > max {
		return fmt.Errorf("%s exceeds %d bytes", name, max)
	}
	return nil
}

// checkURL enforces the host-visible URL contract: only http/https with a host,
// bounded length. The companion re-checks before any navigation; this is the
// host-side gate so a compromised or buggy child cannot be asked to open
// arbitrary schemes from a link.
func checkURL(raw string) error {
	if len(raw) > MaxURLBytes {
		return fmt.Errorf("url exceeds %d bytes", MaxURLBytes)
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("url must be http(s)")
	}
	if len(raw) <= len("https://") {
		return fmt.Errorf("url has no host")
	}
	return nil
}
