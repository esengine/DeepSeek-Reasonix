package boot

import (
	"path/filepath"
	"reasonix/internal/session/control"
	"runtime"
	"strings"

	"reasonix/internal/base/workspaceid"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/browser"
	"reasonix/internal/tools/builtin"
)

// browserWithoutEngine reports whether this host could only ever answer
// browser.engine_missing, so the tools should stay out of the schema rather than
// be offered and then refuse on the first call. A phone is that host: it ships
// no Chromium-family binary, and its WebView is not a process this pool can
// launch and drive.
func browserWithoutEngine(goos, executable string) bool {
	if goos != "android" {
		return false
	}
	return strings.TrimSpace(executable) == ""
}

// browserGoos is the host the engine check answers for. A variable so the branch
// above can be exercised on a machine that is not the host in question: the
// check itself already takes its platform as an argument, and this is the one
// caller that has to read it from the build.
var browserGoos = func() string { return runtime.GOOS }

// browserPool is process-wide: controllers on one workspace share its
// profile, and a profile admits one browser process.
var browserPool = &browser.Pool{}

// bindBrowser gives the browser tools a session when [tools] browser_tools is on:
// reused, the one the replaced runtime had, otherwise a new one. Which browser
// runs is found at first use, so the schema answers to the config alone and a
// machine without one hears browser.engine_missing then.
func bindBrowser(reg *tool.Registry, conf *config.Config, root, profile string, reused *browser.Session) *browser.Session {
	profiles := conf.Roots().BrowserProfilesDir()
	if !conf.Tools.BrowserToolsEnabled() || root == "" || profiles == "" {
		// The tools stay in the schema when the switch is off, and answering
		// `engine_missing` was never the point of turning them off.
		suspendBrowserTools(reg)
		return nil
	}
	cfg := conf.Browser
	if browserWithoutEngine(browserGoos(), cfg.Executable) {
		// Out of the schema, not merely unbound: `addBuiltins` seeds every
		// built-in first, so declining to bind leaves the copies whose only
		// answer is `browser.engine_missing`.
		suspendBrowserTools(reg)
		return nil
	}
	session := reused
	if session == nil {
		session = browser.NewSession(browser.Config{
			Launch: browser.LaunchSpec{
				Executable: cfg.Executable,
				ProfileDir: filepath.Join(profiles, profile),
				Headless:   cfg.Headless,
			},
			Roots: []string{root},
			Pool:  browserPool,
		})
	}
	for _, t := range builtin.BrowserTools(session) {
		reg.Add(t)
	}
	return session
}

// browserProfileKey names the profile a session's browser runs on: its
// workspace's, so the sessions of one project share their logins, or an
// agent's own, so one agent's logins are never another's.
func browserProfileKey(root string, seat control.TeamSeat) string {
	if seat != nil {
		if _, ref := seat.Team(); ref.Agent != "" {
			return "agent-" + ref.Agent
		}
	}
	return strings.ReplaceAll(workspaceid.Key(root), ":", "-")
}

// suspendBrowserTools takes the browser tools out of the registry and keeps them
// out: suspended rather than removed, so a later `Add` — an MCP handshake, a
// preset binding its own set — cannot put the unbound copies straight back.
func suspendBrowserTools(reg *tool.Registry) {
	for _, t := range builtin.BrowserTools(nil) {
		reg.SuspendPrefix(t.Name())
	}
}

// SetBrowserHost routes every browser this process starts through a window
// that draws them, instead of launching one.
func SetBrowserHost(dial browser.EndpointDialer) { browserPool.SetEndpoint(dial) }

// renderRoot is where the executor opens a page it wrote to look at it: the
// workspace, when there is a browser to open it in and a model that can see the
// screenshot. Either missing, a look is nothing it could owe.
func renderRoot(session *browser.Session, entry *config.ProviderEntry, root string) string {
	if session == nil || entry == nil || !config.EffectiveVision(entry) {
		return ""
	}
	return root
}
