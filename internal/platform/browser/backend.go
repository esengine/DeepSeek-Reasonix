package browser

import (
	"context"
)

// BrowserSession is the surface the built-in browser tools call. The desktop
// implementation is *Session, which drives a Chromium process over CDP. On a
// phone the shell's WebView stands in: ShellSession speaks HTTP to it. Both
// satisfy this interface, and the tools are written against it alone, so a
// third backend (a remote browser, a headless farm) can be added without
// touching the tools.
//
// The permission chain is carried by Origin: the tools read it to name the
// site being acted on, and the host's approval gate decides from that name.
// A backend that lies about the origin would bypass the gate, so each one
// reads the URL from the browser itself rather than trusting the caller.
type BrowserSession interface {
	// Navigation and tab management.
	Open(ctx context.Context, rawURL, tabID string, newTab bool) (TabInfo, error)
	Visit(ctx context.Context, rawURL, tabID string, newTab bool) (TabInfo, error)
	VisitBeside(ctx context.Context, rawURL string) (TabInfo, error)
	Switch(tabID string) (TabInfo, error)
	CloseTab(ctx context.Context, tabID string) error
	Tabs() []TabInfo

	// Reading the page.
	Snapshot(ctx context.Context, tabID, scope string) (Snapshot, error)
	Screenshot(ctx context.Context, tabID string) (string, TabInfo, error)
	Logs(tabID string) ([]LogEntry, TabInfo, error)
	Requests(tabID string) ([]Request, TabInfo, error)
	Refresh(ctx context.Context, tabID string) (Snapshot, Changes, error)

	// Acting on the page.
	Act(ctx context.Context, tabID string, steps []Step, secrets bool, scriptOrigin string) (ActResult, error)

	// Identity and provenance, read by the tools' PermissionArgs.
	Origin(tabID string) string
	PageURL(tabID string) string
	ServesWorkspace(url string) bool

	// Credential and script confirmation state. The host's approval gate
	// records which origins have been approved for typing secrets or running
	// scripts; these methods carry that state across calls within one session.
	CredentialEntry(ctx context.Context, tabID string, steps []Step) bool
	NoteSecretCheck(args []byte, secret bool)
	NoteScriptOrigin(args []byte, origin string)
	SecretsConfirmed(args []byte) bool
	ScriptOrigin(args []byte) string

	// Lifecycle.
	OnTabsChanged(fn func())
	Retain()
	Release()
	Close()
}
