package main

import (
	"fmt"
	"runtime"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/sandbox"
)

// Shell support discovery and repair guidance. The Git.Git winget manifest may
// elevate even with user scope, so Reasonix never launches that installer and
// Windows exposes only the official manual link.

// shellInstallActionGitForWindows is the single install action id hosts may
// request today; unknown ids are rejected as errors rather than no-ops so a
// frontend typo cannot silently do nothing.
const shellInstallActionGitForWindows = "git-for-windows"

// GitForWindowsManualURL is the official download page handed to Windows users.
const GitForWindowsManualURL = "https://git-scm.com/download/win"

// Structured outcomes retained by the Wails contract. Invalid action ids remain
// errors; supported Windows requests always return manual_required.
const (
	shellInstallStatusManualRequired = "manual_required"
	shellInstallStatusUnsupported    = "unsupported_platform"
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

// ShellInstallActionView describes Windows manual repair guidance. It remains
// shaped as an action for Wails compatibility, but Available is false and Mode
// is always "manual". Nil on macOS and Linux, which use repair guidance.
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

// InstallShellSupport retains the existing Wails method while enforcing the
// manual-only policy. It never probes for or launches a package manager.
func (a *App) InstallShellSupport(id string) (ShellInstallResult, error) {
	return installShellSupportForGOOS(runtime.GOOS, id)
}

func installShellSupportForGOOS(goos, id string) (ShellInstallResult, error) {
	if strings.TrimSpace(id) != shellInstallActionGitForWindows {
		return ShellInstallResult{}, fmt.Errorf("unknown shell support action %q", id)
	}
	if goos != "windows" {
		return ShellInstallResult{
			Status: shellInstallStatusUnsupported,
			Reason: "shell helper install is only available on Windows",
		}, nil
	}
	return ShellInstallResult{
		Status:    shellInstallStatusManualRequired,
		Reason:    "automatic installation is disabled because Git for Windows cannot reliably honor user scope",
		ManualURL: GitForWindowsManualURL,
	}, nil
}

// CancelShellInstall is retained as an idempotent compatibility no-op. No
// installer can be running under the manual-only policy.
func (a *App) CancelShellInstall() {}

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

// shellInstallActionViewForGOOS exposes the official manual link on Windows.
// Other platforms use their native detect-and-guide policy.
func shellInstallActionViewForGOOS(goos string) *ShellInstallActionView {
	if goos != "windows" {
		return nil
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
// machine state. They can diverge after a manual repair or an unreloaded config
// edit, so the surface shows both plus an explicit reload button.
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
		ShellCapabilities:   sandboxCapabilityViews(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path),
		GitCapability:       gitCapabilityView(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path),
		ShellInstallAction:  shellInstallActionViewForGOOS(runtime.GOOS),
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

func gitCapabilityView(prefer, configPath string) *ShellCapabilityView {
	capability := sandbox.GitCapabilityForConfig(prefer, configPath)
	return &ShellCapabilityView{
		ID: capability.ID, Available: capability.Available, Path: capability.Path,
		Source: capability.Source, Reason: capability.Reason,
	}
}

// sandboxCapabilityViews projects the discovered shell inventory for the
// settings surface. The slice is never nil so the Wails binding always
// encodes a JSON array, not null.
func sandboxCapabilityViews(prefer, configPath string) []ShellCapabilityView {
	caps := sandbox.ShellCapabilitiesForConfig(prefer, configPath)
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
