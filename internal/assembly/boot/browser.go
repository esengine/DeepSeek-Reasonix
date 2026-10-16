package boot

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/base/workspaceid"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/browser"
	"reasonix/internal/tools/builtin"
)

// browserPool is process-wide: controllers on one workspace share its
// profile, and a profile admits one browser process.
var browserPool = &browser.Pool{}

// shellBrowserPort is the env var the Kotlin shell sets to name the port its
// browser HTTP server listens on. When it is present the kernel binds the
// browser tools to the shell's WebView instead of looking for a Chromium
// binary that does not exist on a phone.
const shellBrowserPortEnv = "REASONIX_SHELL_BROWSER_PORT"

// shellBrowserTokenEnv is the shared secret the shell minted for the browser
// server it started. The kernel sends it as a bearer credential on every
// call; the shell refuses a request that does not carry it.
//
// It is a separate variable rather than something discovered on loopback
// because loopback is not a boundary on a phone: any other app can connect to
// 127.0.0.1 on the port this server binds, and the routes behind it run
// script in and read text out of the loaded page. Naming the secret in the
// child's environment is what makes the port reachable only by the kernel
// this shell started.
const shellBrowserTokenEnv = "REASONIX_SHELL_BROWSER_TOKEN"

// browserWithoutEngine reports whether this host could only ever answer
// browser.engine_missing, so the tools should stay out of the schema rather
// than be offered and then refuse on the first call.
//
// Android is that host when the shell has not provided a browser server: it
// ships no Chromium-family binary, and its WebView is not a process this pool
// can launch and drive. When the shell does provide one
// (REASONIX_SHELL_BROWSER_PORT is set), the tools bind to it instead.
func browserWithoutEngine(goos, executable string) bool {
	if goos != "android" {
		return false
	}
	if strings.TrimSpace(executable) != "" {
		return false
	}
	return os.Getenv(shellBrowserPortEnv) == ""
}

// browserGoos is the host the engine check answers for. A variable so the
// Android branch can be exercised on a machine that is not a phone: the check
// itself already takes its platform as an argument, and this is the one caller
// that has to read it from the build.
var browserGoos = func() string { return runtime.GOOS }

// bindBrowser gives the browser tools a session when [tools] browser_tools is on:
// reused, the one the replaced runtime had, otherwise a new one. Which browser
// runs is found at first use, so the schema answers to the config alone and a
// machine without one hears browser.engine_missing then.
//
// On Android the browser is the shell's WebView, reached over loopback HTTP.
// The shell names the port in REASONIX_SHELL_BROWSER_PORT; when the variable
// is absent the tools stay suspended, because there is no Chromium to drive.
func bindBrowser(reg *tool.Registry, conf *config.Config, root string, reused *browser.Session) *browser.Session {
	profiles := conf.Roots().BrowserProfilesDir()
	if !conf.Tools.BrowserToolsEnabled() || root == "" || profiles == "" {
		// The tools stay in the schema when the switch is off, and answering
		// `engine_missing` was never the point of turning them off.
		suspendBrowserTools(reg)
		return nil
	}
	cfg := conf.Browser

	// On Android with a shell browser server, bind to the WebView.
	// NewSessionFromShell returns a *Session whose methods delegate to the
	// shell's HTTP endpoints, so builtin.BrowserTools works unchanged.
	if browserGoos() == "android" {
		if port := os.Getenv(shellBrowserPortEnv); port != "" {
			base := "http://127.0.0.1:" + port
			session := browser.NewSessionFromShell(base, []string{root}, os.Getenv(shellBrowserTokenEnv))
			for _, t := range builtin.BrowserTools(session) {
				reg.Add(t)
			}
			return session
		}
	}

	if browserWithoutEngine(browserGoos(), cfg.Executable) {
		// Out of the schema, not merely unbound.
		//
		// `addBuiltins` seeds every registered built-in, so the three browser
		// tools are in the registry before this runs. Not binding them left the
		// unbound copies in place — the ones whose only answer is
		// `browser.engine_missing` — and the model, seeing them offered, called
		// them and was told to install a browser Android cannot install. On a
		// phone that is the whole of what a person saw.
		suspendBrowserTools(reg)
		return nil
	}
	session := reused
	if session == nil {
		session = browser.NewSession(browser.Config{
			Launch: browser.LaunchSpec{
				Executable: cfg.Executable,
				ProfileDir: filepath.Join(profiles, strings.ReplaceAll(workspaceid.Key(root), ":", "-")),
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

// suspendBrowserTools takes the browser tools out of the registry and keeps
// them out.
//
// Suspended rather than removed: a later `Add` — an MCP handshake, a preset
// that binds its own set — would otherwise put the unbound copies straight
// back, and the refusal would return a release later with nothing to point at.
// The names come from `builtin.BrowserTools` so a fourth tool cannot be added
// without landing here.
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
