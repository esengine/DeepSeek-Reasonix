package main

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/sandbox"
)

// assertConfigUntouched proves the repair API contract on every branch: the
// user config file is byte-identical to before, so no branch can smuggle in
// prefer="bash" or rebuild the controller.
func assertConfigUntouched(t *testing.T, before string) {
	t.Helper()
	if after := readUserConfigOrEmpty(t); after != before {
		t.Fatalf("install branch rewrote config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func readUserConfigOrEmpty(t *testing.T) string {
	t.Helper()
	path := config.UserConfigPath()
	if path == "" {
		t.Fatal("user config path unavailable in test env")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "" // no file yet: absence is also a comparable state
	}
	return string(raw)
}

func TestInstallShellSupportRejectsUnknownAction(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	if _, err := app.InstallShellSupport("homebrew"); err == nil {
		t.Fatal("unknown action id must be a Go error, not a structured result")
	}
}

func TestInstallShellSupportPolicy(t *testing.T) {
	isolateDesktopUserDirs(t)
	before := readUserConfigOrEmpty(t)

	manual, err := installShellSupportForGOOS("windows", shellInstallActionGitForWindows)
	if err != nil {
		t.Fatalf("Windows manual fallback must be structured, not an error: %v", err)
	}
	if manual.Status != shellInstallStatusManualRequired || manual.ManualURL != GitForWindowsManualURL {
		t.Fatalf("Windows result = %+v, want manual_required with the official link", manual)
	}
	if !strings.Contains(manual.Reason, "automatic installation is disabled") {
		t.Fatalf("Windows manual reason = %q, want the safety policy", manual.Reason)
	}

	for _, goos := range []string{"darwin", "linux"} {
		unsupported, err := installShellSupportForGOOS(goos, shellInstallActionGitForWindows)
		if err != nil {
			t.Fatalf("%s unsupported result must be structured: %v", goos, err)
		}
		if unsupported.Status != shellInstallStatusUnsupported {
			t.Fatalf("%s status = %q, want %q", goos, unsupported.Status, shellInstallStatusUnsupported)
		}
	}

	if _, err := installShellSupportForGOOS("windows", "homebrew"); err == nil {
		t.Fatal("unknown action id must be rejected before platform policy")
	}
	assertConfigUntouched(t, before)
}

func TestSetShellPreferencePreservesOtherFields(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Tools.Shell.Prefer = "auto"
	cfg.Tools.Shell.Path = `C:\Custom\bin\bash.exe`
	cfg.Sandbox.Network = true
	cfg.Sandbox.WorkspaceRoot = `D:\work`
	cfg.Sandbox.AllowWrite = []string{`E:\extra`}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	app := NewApp()
	if err := app.SetShellPreference("powershell"); err != nil {
		t.Fatalf("SetShellPreference: %v", err)
	}

	got := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	if got.Tools.Shell.Prefer != "powershell" {
		t.Fatalf("prefer = %q, want powershell", got.Tools.Shell.Prefer)
	}
	if got.Tools.Shell.Path != `C:\Custom\bin\bash.exe` {
		t.Fatalf("shell path = %q, want preserved", got.Tools.Shell.Path)
	}
	if !got.Sandbox.Network || got.Sandbox.WorkspaceRoot != `D:\work` || !reflect.DeepEqual(got.Sandbox.AllowWrite, []string{`E:\extra`}) {
		t.Fatalf("sandbox fields were rewritten: %+v", got.Sandbox)
	}

	if err := app.SetShellPreference("fish"); err == nil {
		t.Fatal("invalid preference must be rejected as a Go error")
	}
}

func TestShellInstallActionViewPerPlatform(t *testing.T) {
	if got := shellInstallActionViewForGOOS("darwin"); got != nil {
		t.Fatalf("darwin action = %+v, want nil", got)
	}
	if got := shellInstallActionViewForGOOS("linux"); got != nil {
		t.Fatalf("linux action = %+v, want nil", got)
	}
	manual := shellInstallActionViewForGOOS("windows")
	if manual == nil || manual.Mode != "manual" || manual.Available || manual.ManualURL != GitForWindowsManualURL {
		t.Fatalf("windows manual action = %+v, want unavailable manual with the official link", manual)
	}
}

func TestSettingsSandboxViewShellContract(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	cfg.Tools.Shell.Prefer = "powershell"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save initial config: %v", err)
	}
	app := NewApp()
	view := app.Settings()
	sb := view.Sandbox
	if sb.Shell != "powershell" {
		t.Fatalf("shell = %q, want powershell", sb.Shell)
	}
	// Without a live controller the session shell is the fresh resolution, so
	// the two views agree and no reload is implied.
	if sb.EffectiveShell == "" || sb.EffectiveShell != sb.ResolvedShell {
		t.Fatalf("effective = %q resolved = %q, want equal fallbacks", sb.EffectiveShell, sb.ResolvedShell)
	}
	if sb.ShellReloadRequired {
		t.Fatal("no controller divergence without a controller")
	}
	// Wails must encode an array, never null.
	if sb.ShellCapabilities == nil {
		t.Fatal("shellCapabilities must never be nil")
	}
	if len(sb.ShellCapabilities) == 0 {
		t.Fatal("shellCapabilities should report at least one interpreter")
	}
	if sb.GitCapability == nil || sb.GitCapability.ID != sandbox.HostCapabilityGit {
		t.Fatalf("Git capability = %+v, want independent Git report", sb.GitCapability)
	}
	for _, cap := range sb.ShellCapabilities {
		if cap.ID == "" {
			t.Fatalf("capability without id: %+v", cap)
		}
		if cap.Available && cap.Path == "" {
			t.Fatalf("available capability %q without a path", cap.ID)
		}
	}
	if runtime.GOOS != "windows" && sb.ShellInstallAction != nil {
		t.Fatalf("install action on %s = %+v, want nil", runtime.GOOS, sb.ShellInstallAction)
	}
	switch runtime.GOOS {
	case "windows":
		if sb.ShellRepairGuidance != nil {
			t.Fatalf("repair guidance on Windows = %+v, want install action only", sb.ShellRepairGuidance)
		}
		if sb.GitRepairGuidance != nil {
			t.Fatalf("Git repair guidance on Windows = %+v, want Git for Windows action only", sb.GitRepairGuidance)
		}
	case "darwin":
		if sb.ShellRepairGuidance != nil {
			t.Fatalf("macOS shell guidance = %+v, want native zsh/sh fallback", sb.ShellRepairGuidance)
		}
		if sb.GitRepairGuidance == nil || sb.GitRepairGuidance.Command != "brew install git" {
			t.Fatalf("macOS Git guidance = %+v, want Homebrew Git command", sb.GitRepairGuidance)
		}
	default:
		if sb.ShellRepairGuidance == nil {
			t.Fatalf("repair guidance on %s is nil", runtime.GOOS)
		}
		if strings.Contains(strings.ToLower(sb.ShellRepairGuidance.Command), "sudo") {
			t.Fatalf("repair guidance must never prescribe sudo: %+v", sb.ShellRepairGuidance)
		}
		if sb.GitRepairGuidance == nil || strings.Contains(strings.ToLower(sb.GitRepairGuidance.Command), "sudo") {
			t.Fatalf("Git repair guidance must exist without sudo: %+v", sb.GitRepairGuidance)
		}
	}
}
