package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/proc"
	"reasonix/internal/sandbox"
)

// Shell support helper install: the desktop's user-triggered "install Git for
// Windows" action. Only Windows installs anything, and installing never
// mutates [tools.shell] or rebuilds anything — the user reloads explicitly.

// shellInstallActionGitForWindows is the single install action id hosts may
// request today; unknown ids are rejected as errors rather than no-ops so a
// frontend typo cannot silently do nothing.
const shellInstallActionGitForWindows = "git-for-windows"

// GitForWindowsManualURL is the official download page handed to the user when
// winget cannot run the install (missing App Installer, user scope refused, …).
const GitForWindowsManualURL = "https://git-scm.com/download/win"

// Structured install outcomes. Everything except invalid requests and internal
// bridge failures comes back as one of these — a cancelled or manual-required
// install is a normal result, not an error.
const (
	shellInstallStatusInstalled        = "installed"
	shellInstallStatusAlreadyAvailable = "already_available"
	shellInstallStatusCancelled        = "cancelled"
	shellInstallStatusManualRequired   = "manual_required"
	shellInstallStatusBusy             = "busy"
	shellInstallStatusFailed           = "failed"
	shellInstallStatusUnsupported      = "unsupported_platform"
)

// ShellInstallResult is the structured outcome of InstallShellSupport.
type ShellInstallResult struct {
	Status    string `json:"status"`
	Path      string `json:"path,omitempty"`
	Reason    string `json:"reason,omitempty"`
	ManualURL string `json:"manualUrl,omitempty"`
}

// ShellCapabilityView is one discovered interpreter for the settings surface:
// whether it is usable, where it lives, how it was found, and why not when
// unavailable. Purely informational — resolution goes through ResolveShell.
type ShellCapabilityView struct {
	ID        string `json:"id"`
	Variant   string `json:"variant,omitempty"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
	Source    string `json:"source,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// ShellInstallActionView describes the optional helper install the settings
// surface may offer: "winget-user" means a one-click user-scope install is
// available, "manual" means only the official link can be offered. Nil on
// platforms that detect-and-guide only (macOS, Linux).
type ShellInstallActionView struct {
	ID        string `json:"id"`
	Mode      string `json:"mode"`
	Available bool   `json:"available"`
	ManualURL string `json:"manualUrl,omitempty"`
}

// SandboxView is the Settings panel's sandbox surface. The shell fields
// separate three states: the configured preference (Shell), what the live
// controller bound (EffectiveShell), and what a reload would pick now
// (ResolvedShell) — ShellReloadRequired marks the divergence.
type SandboxView struct {
	Bash                   string                  `json:"bash"`
	Network                bool                    `json:"network"`
	WorkspaceRoot          string                  `json:"workspaceRoot"`
	AllowWrite             []string                `json:"allowWrite"`
	EffectiveWorkspaceRoot string                  `json:"effectiveWorkspaceRoot"`
	EffectiveWriteRoots    []string                `json:"effectiveWriteRoots"`
	Shell                  string                  `json:"shell"` // [tools.shell] prefer: auto|bash|powershell|pwsh
	EffectiveShell         string                  `json:"effectiveShell,omitempty"`
	ResolvedShell          string                  `json:"resolvedShell,omitempty"`
	ShellReloadRequired    bool                    `json:"shellReloadRequired"`
	ShellCapabilities      []ShellCapabilityView   `json:"shellCapabilities"`
	GitCapability          *ShellCapabilityView    `json:"gitCapability,omitempty"`
	ShellInstallAction     *ShellInstallActionView `json:"shellInstallAction,omitempty"`
	ShellRepairGuidance    *RepairGuidanceView     `json:"shellRepairGuidance,omitempty"`
	GitRepairGuidance      *RepairGuidanceView     `json:"gitRepairGuidance,omitempty"`
}

// shellInstallManager serializes helper installs: at most one installer child
// process at a time, with an idempotent cancel shared with app shutdown.
type shellInstallManager struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

// begin claims the install slot and returns the run context plus a done
// function that must be called when the install finishes. ok is false when an
// install is already running.
func (m *shellInstallManager) begin() (ctx context.Context, done func(), ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return ctx, func() {
		cancel()
		m.mu.Lock()
		m.cancel = nil
		m.mu.Unlock()
	}, true
}

// cancelAll terminates the in-flight install, if any. Idempotent: cancelling
// with nothing running is a no-op, which is what the settings cancel button
// and app shutdown both need.
func (m *shellInstallManager) cancelAll() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// shellInstallTimeout bounds one winget run. Git for Windows is ~60 MB, so a
// slow link can legitimately take minutes; ten covers it without leaving a
// hung install owning the slot forever.
const shellInstallTimeout = 10 * time.Minute

// wingetInstallArgv is the fixed winget command line. It installs only the
// official Git.Git package from the winget source at user scope, silently,
// with both agreements pre-accepted. Never broaden it to machine scope or
// another source: a helper install must not escalate privileges behind the
// user's back.
func wingetInstallArgv() []string {
	return []string{
		"install",
		"--id", "Git.Git",
		"--exact",
		"--source", "winget",
		"--scope", "user",
		"--silent",
		"--accept-source-agreements",
		"--accept-package-agreements",
	}
}

// shellInstallBackend is the platform seam behind the install flow so every
// branch (winget missing, failure, cancel, timeout, post-install verify) is
// testable on any host without spawning real installs.
type shellInstallBackend struct {
	// platformSupported reports whether this OS offers the install at all.
	platformSupported func() bool
	// probe re-detects the git-bash capability as it stands right now while
	// honoring the configured shell path (for portable Git installations).
	probe func(configPath string) sandbox.ShellCapability
	// winget resolves the winget executable; "" when it is not available.
	winget func() string
	// run executes the install under ctx; cancelling ctx must terminate the
	// child process.
	run func(ctx context.Context, winget string, argv []string) error
	// timeout bounds one run; tests shorten it.
	timeout time.Duration
}

// shellInstallBackendForGOOS returns the production backend for goos. Windows
// wires winget; everywhere else the install is unsupported because bash is an
// OS component, not a side-load.
func shellInstallBackendForGOOS(goos string) shellInstallBackend {
	if goos != "windows" {
		return shellInstallBackend{platformSupported: func() bool { return false }}
	}
	return shellInstallBackend{
		platformSupported: func() bool { return true },
		probe:             gitBashCapability,
		winget: func() string {
			if p, err := wingetLookPath(); err == nil {
				return p
			}
			return ""
		},
		run:     runWingetInstall,
		timeout: shellInstallTimeout,
	}
}

// resolveShellInstallBackend yields the backend for the current install; tests
// stub it to drive every branch without spawning a real winget.
var resolveShellInstallBackend = func() shellInstallBackend {
	return shellInstallBackendForGOOS(runtime.GOOS)
}

// wingetLookPath resolves App Installer's winget.exe. It lives under
// %LOCALAPPDATA%\Microsoft\WindowsApps, which is on the user PATH for normal
// GUI sessions; LookPath is the honest availability check (and fails fast
// anywhere winget does not exist).
func wingetLookPath() (string, error) {
	return exec.LookPath("winget")
}

func wingetAvailable() bool {
	_, err := wingetLookPath()
	return err == nil
}

// runWingetInstall executes the fixed argv with no shell in between, a hidden
// window, and the child's lifetime tied to ctx: user cancel or app exit
// terminates winget instead of orphaning it.
func runWingetInstall(ctx context.Context, winget string, argv []string) error {
	cmd := proc.CommandContext(ctx, winget, argv...)
	_, err := proc.RunCommand(ctx, cmd, proc.RunOptions{
		Track:          true,
		Source:         "desktop-shell-support",
		CommandPreview: "winget install Git.Git --scope user",
	})
	return err
}

// gitBashCapability re-probes discovery and returns the git-bash capability
// as it stands right now: the cache is invalidated first so a just-finished
// install is actually seen.
func gitBashCapability(configPath string) sandbox.ShellCapability {
	sandbox.InvalidateShellInventory()
	for _, cap := range sandbox.ShellCapabilitiesForPath(configPath) {
		if cap.ID == sandbox.ShellCapabilityGitBash {
			return cap
		}
	}
	return sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
}

// InstallShellSupport runs the requested helper install. Only the Windows
// desktop offers git-for-windows; other platforms get a structured
// unsupported result because Bash there is an OS component, not a side-load.
func (a *App) InstallShellSupport(id string) (ShellInstallResult, error) {
	if strings.TrimSpace(id) != shellInstallActionGitForWindows {
		return ShellInstallResult{}, fmt.Errorf("unknown shell support action %q", id)
	}
	return a.installGitForWindows()
}

// CancelShellInstall cancels the in-flight helper install, if any. Idempotent.
func (a *App) CancelShellInstall() {
	a.shellInstall.cancelAll()
}

// installGitForWindows runs one serialized, cancellable, timeout-bounded
// install. When it returns it has cleared the discovery cache and verified
// bash.exe — and nothing else: no config write, no controller rebuild, no
// shell switch. The settings surface shows the new capability and lets the
// user reload the session explicitly.
func (a *App) installGitForWindows() (ShellInstallResult, error) {
	b := resolveShellInstallBackend()
	if !b.platformSupported() {
		return ShellInstallResult{
			Status: shellInstallStatusUnsupported,
			Reason: "shell helper install is only available on Windows",
		}, nil
	}
	configuredPath := ""
	if cfg, _, err := a.loadDesktopUserConfigForView(); err == nil {
		configuredPath = cfg.Tools.Shell.Path
	}

	// Re-probe first: an already-working Git Bash needs no install.
	if cap := b.probe(configuredPath); cap.Available {
		return ShellInstallResult{
			Status: shellInstallStatusAlreadyAvailable,
			Path:   cap.Path,
		}, nil
	}

	winget := b.winget()
	if winget == "" {
		// App Installer (winget) ships with the Store; LTSC and stripped
		// builds lack it. Installing App Installer to install Git would be a
		// privilege-escalating rabbit hole — hand back the manual link.
		return ShellInstallResult{
			Status:    shellInstallStatusManualRequired,
			Reason:    "winget (App Installer) is not available on this system",
			ManualURL: GitForWindowsManualURL,
		}, nil
	}

	ctx, done, ok := a.shellInstall.begin()
	if !ok {
		return ShellInstallResult{
			Status: shellInstallStatusBusy,
			Reason: "another shell support install is already running",
		}, nil
	}
	defer done()

	timeout := b.timeout
	if timeout <= 0 {
		timeout = shellInstallTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	runErr := b.run(ctx, winget, wingetInstallArgv())

	if ctx.Err() != nil {
		if ctx.Err() == context.Canceled {
			return ShellInstallResult{Status: shellInstallStatusCancelled}, nil
		}
		return ShellInstallResult{
			Status:    shellInstallStatusFailed,
			Reason:    fmt.Sprintf("winget install timed out after %s", timeout),
			ManualURL: GitForWindowsManualURL,
		}, nil
	}
	if runErr != nil {
		// Only the exit status is reported: full winget output can embed
		// machine paths and stays out of UI surfaces.
		return ShellInstallResult{
			Status:    shellInstallStatusFailed,
			Reason:    fmt.Sprintf("winget install failed: %v", runErr),
			ManualURL: GitForWindowsManualURL,
		}, nil
	}

	// winget finished: clear the discovery cache and verify bash.exe exists
	// before claiming success.
	if cap := b.probe(configuredPath); cap.Available {
		return ShellInstallResult{
			Status: shellInstallStatusInstalled,
			Path:   cap.Path,
		}, nil
	}
	return ShellInstallResult{
		Status:    shellInstallStatusFailed,
		Reason:    "installation completed but Git Bash was not detected yet; restart Reasonix and check " + GitForWindowsManualURL,
		ManualURL: GitForWindowsManualURL,
	}, nil
}

// SetShellPreference updates only [tools.shell] prefer, preserving the
// configured shell path and every other sandbox field, so switching the
// interpreter never rewrites unrelated settings.
func (a *App) SetShellPreference(prefer string) error {
	prefer = strings.TrimSpace(prefer)
	switch strings.ToLower(prefer) {
	case "", "auto", "bash", "powershell", "pwsh":
	default:
		return fmt.Errorf("invalid shell preference %q (use auto, bash, powershell, or pwsh)", prefer)
	}
	return a.applyConfigChange(func(c *config.Config) error {
		c.Tools.Shell.Prefer = prefer
		return nil
	})
}

// shellInstallActionViewForGOOS reports whether this platform offers an
// install action for the settings surface. Windows offers the winget user
// scope install when App Installer is present, else the manual link; other
// platforms detect and guide only.
func shellInstallActionViewForGOOS(goos string, wingetAvailable bool) *ShellInstallActionView {
	if goos != "windows" {
		return nil
	}
	if wingetAvailable {
		return &ShellInstallActionView{
			ID:        shellInstallActionGitForWindows,
			Mode:      "winget-user",
			Available: true,
		}
	}
	return &ShellInstallActionView{
		ID:        shellInstallActionGitForWindows,
		Mode:      "manual",
		Available: false,
		ManualURL: GitForWindowsManualURL,
	}
}

// sandboxViewFor builds the Settings panel's SandboxView shell surface.
// effectiveShell is what the live controller actually bound at build time;
// resolvedShell is what a reload would pick from the current config and
// machine state. They diverge after an install or an unreloaded config edit,
// so the surface shows both plus an explicit reload button.
func (a *App) sandboxViewFor(cfg *config.Config, ctrl control.SessionAPI, writeRoots []string, effectiveWorkspaceRoot string) SandboxView {
	shell := cfg.Tools.Shell.Prefer
	if shell == "" {
		shell = "auto"
	}
	resolved := sandbox.ResolveShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path, nil)
	bound := resolved
	if ctrl != nil {
		if sh := ctrl.BoundShell(); sh.Path != "" {
			bound = sh
		}
	}
	return SandboxView{
		Bash: cfg.BashMode(), Network: cfg.Sandbox.Network,
		WorkspaceRoot: cfg.Sandbox.WorkspaceRoot, AllowWrite: nonNil(cfg.Sandbox.AllowWrite),
		EffectiveWorkspaceRoot: effectiveWorkspaceRoot, EffectiveWriteRoots: nonNil(writeRoots),
		Shell: shell, EffectiveShell: sandboxEffectiveShellView(bound),
		ResolvedShell:       sandboxEffectiveShellView(resolved),
		ShellReloadRequired: bound != resolved,
		ShellCapabilities:   sandboxCapabilityViews(cfg.Tools.Shell.Path),
		GitCapability:       gitCapabilityView(cfg.Tools.Shell.Path),
		ShellInstallAction:  shellInstallActionViewForGOOS(runtime.GOOS, wingetAvailable()),
		ShellRepairGuidance: shellRepairGuidanceForGOOS(runtime.GOOS),
		GitRepairGuidance:   gitRepairGuidanceForGOOS(runtime.GOOS),
	}
}

func sandboxEffectiveShellView(sh sandbox.Shell) string {
	if sh.Kind == sandbox.ShellZsh {
		return "zsh"
	}
	if sh.Kind == sandbox.ShellSh {
		return "sh"
	}
	if sh.Kind == sandbox.ShellPowerShell {
		if sh.SupportsChaining() {
			return "pwsh"
		}
		return "powershell"
	}
	path := strings.ToLower(strings.ReplaceAll(sh.Path, "\\", "/"))
	if strings.Contains(path, "/git/") && strings.HasSuffix(path, "bash.exe") {
		return "git-bash"
	}
	return "bash"
}

func gitCapabilityView(configPath string) *ShellCapabilityView {
	capability := sandbox.GitCapabilityForPath(configPath)
	return &ShellCapabilityView{
		ID: capability.ID, Available: capability.Available, Path: capability.Path,
		Source: capability.Source, Reason: capability.Reason,
	}
}

// sandboxCapabilityViews projects the discovered shell inventory for the
// settings surface. The slice is never nil so the Wails binding always
// encodes a JSON array, not null.
func sandboxCapabilityViews(configPath string) []ShellCapabilityView {
	caps := sandbox.ShellCapabilitiesForPath(configPath)
	out := make([]ShellCapabilityView, 0, len(caps))
	for _, cap := range caps {
		out = append(out, ShellCapabilityView{
			ID:        cap.ID,
			Variant:   cap.Variant,
			Available: cap.Available,
			Path:      cap.Path,
			Source:    cap.Source,
			Reason:    cap.Reason,
		})
	}
	return out
}
