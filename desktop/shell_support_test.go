package main

import (
	"context"
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/sandbox"
)

// stubBackend drives installGitForWindows through every branch without
// spawning a real winget. probeResults is consumed one entry per probe call so
// a test can model "missing before install, present after".
func stubBackend(backend *shellInstallBackend, probeResults chan sandbox.ShellCapability, runErr error, timeout time.Duration) {
	*backend = shellInstallBackend{
		platformSupported: func() bool { return true },
		probe: func(_ string) sandbox.ShellCapability {
			select {
			case cap := <-probeResults:
				return cap
			default:
				return sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
			}
		},
		winget:  func() string { return `C:\fake\winget.exe` },
		run:     func(ctx context.Context, winget string, argv []string) error { return runErr },
		timeout: timeout,
	}
}

func withStubbedBackend(t *testing.T, b shellInstallBackend) {
	t.Helper()
	prev := resolveShellInstallBackend
	resolveShellInstallBackend = func() shellInstallBackend { return b }
	t.Cleanup(func() { resolveShellInstallBackend = prev })
}

// assertConfigUntouched proves the installer contract on every branch: the
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

func TestWingetInstallArgvIsFixedUserScope(t *testing.T) {
	// The exact command line is a security contract: official package, winget
	// source, user scope, silent, both agreements pre-accepted — and nothing
	// else. Any change here needs review, not a quiet edit.
	want := []string{
		"install",
		"--id", "Git.Git",
		"--exact",
		"--source", "winget",
		"--scope", "user",
		"--silent",
		"--accept-source-agreements",
		"--accept-package-agreements",
	}
	if got := wingetInstallArgv(); !reflect.DeepEqual(got, want) {
		t.Fatalf("winget argv = %v\nwant %v", got, want)
	}
}

func TestInstallShellSupportBranches(t *testing.T) {
	t.Run("unsupported platform", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		withStubbedBackend(t, shellInstallBackend{platformSupported: func() bool { return false }})
		app := NewApp()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("unsupported platform is structured, not an error: %v", err)
		}
		if res.Status != shellInstallStatusUnsupported {
			t.Fatalf("status = %q, want %q", res.Status, shellInstallStatusUnsupported)
		}
		assertConfigUntouched(t, before)
	})

	t.Run("already available", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		probes := make(chan sandbox.ShellCapability, 4)
		probes <- sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash, Available: true, Path: `C:\Git\bin\bash.exe`}
		b := shellInstallBackend{}
		stubBackend(&b, probes, nil, time.Minute)
		withStubbedBackend(t, b)
		app := NewApp()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("already_available must not error: %v", err)
		}
		if res.Status != shellInstallStatusAlreadyAvailable || res.Path != `C:\Git\bin\bash.exe` {
			t.Fatalf("res = %+v, want already_available with the probed path", res)
		}
		assertConfigUntouched(t, before)
	})

	t.Run("configured portable Git Bash is already available", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		cfg := config.Default()
		cfg.Tools.Shell.Path = `D:\PortableGit\git-bash.exe`
		if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
			t.Fatalf("save portable shell config: %v", err)
		}
		before := readUserConfigOrEmpty(t)
		var probedPath string
		wingetCalled := false
		runCalled := false
		withStubbedBackend(t, shellInstallBackend{
			platformSupported: func() bool { return true },
			probe: func(configPath string) sandbox.ShellCapability {
				probedPath = configPath
				return sandbox.ShellCapability{
					ID:        sandbox.ShellCapabilityGitBash,
					Available: true,
					Path:      `D:\PortableGit\bin\bash.exe`,
				}
			},
			winget: func() string {
				wingetCalled = true
				return `C:\fake\winget.exe`
			},
			run: func(context.Context, string, []string) error {
				runCalled = true
				return nil
			},
		})

		app := NewApp()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("portable Git Bash preflight: %v", err)
		}
		if res.Status != shellInstallStatusAlreadyAvailable || res.Path != `D:\PortableGit\bin\bash.exe` {
			t.Fatalf("res = %+v, want portable Git Bash already_available", res)
		}
		if probedPath != cfg.Tools.Shell.Path {
			t.Fatalf("probe path = %q, want configured path %q", probedPath, cfg.Tools.Shell.Path)
		}
		if wingetCalled || runCalled {
			t.Fatalf("portable Git Bash must skip winget: winget=%v run=%v", wingetCalled, runCalled)
		}
		assertConfigUntouched(t, before)
	})

	t.Run("winget missing requires manual", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		b := shellInstallBackend{
			platformSupported: func() bool { return true },
			probe: func(string) sandbox.ShellCapability {
				return sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
			},
			winget: func() string { return "" },
		}
		withStubbedBackend(t, b)
		app := NewApp()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("manual_required must not error: %v", err)
		}
		if res.Status != shellInstallStatusManualRequired {
			t.Fatalf("status = %q, want manual_required", res.Status)
		}
		if res.ManualURL != GitForWindowsManualURL {
			t.Fatalf("manualUrl = %q, want the official Git for Windows link", res.ManualURL)
		}
		assertConfigUntouched(t, before)
	})

	t.Run("installed and verified", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		probes := make(chan sandbox.ShellCapability, 4)
		probes <- sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
		probes <- sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash, Available: true, Path: `C:\Program Files\Git\bin\bash.exe`}
		b := shellInstallBackend{}
		stubBackend(&b, probes, nil, time.Minute)
		withStubbedBackend(t, b)
		app := NewApp()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("installed must not error: %v", err)
		}
		if res.Status != shellInstallStatusInstalled || res.Path != `C:\Program Files\Git\bin\bash.exe` {
			t.Fatalf("res = %+v, want installed with the verified path", res)
		}
		assertConfigUntouched(t, before)
		// The install slot must be free again.
		if ctx, done, ok := app.shellInstall.begin(); !ok {
			t.Fatal("install slot still held after completion")
		} else {
			done()
			<-ctx.Done()
		}
	})

	t.Run("run failure", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		probes := make(chan sandbox.ShellCapability, 4)
		probes <- sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
		b := shellInstallBackend{}
		stubBackend(&b, probes, errors.New("exit status 0x8a150014"), time.Minute)
		withStubbedBackend(t, b)
		app := NewApp()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("run failure must be structured, not a Go error: %v", err)
		}
		if res.Status != shellInstallStatusFailed || !strings.Contains(res.Reason, "exit status") {
			t.Fatalf("res = %+v, want failed with the exit status reason", res)
		}
		if res.ManualURL == "" {
			t.Fatal("failed install should still offer the manual link")
		}
		assertConfigUntouched(t, before)
	})

	t.Run("timeout", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		probes := make(chan sandbox.ShellCapability, 4)
		probes <- sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
		b := shellInstallBackend{}
		stubBackend(&b, probes, nil, 30*time.Millisecond)
		b.run = func(ctx context.Context, winget string, argv []string) error {
			<-ctx.Done() // a winget that never finishes on its own
			return ctx.Err()
		}
		withStubbedBackend(t, b)
		app := NewApp()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("timeout must be structured, not a Go error: %v", err)
		}
		if res.Status != shellInstallStatusFailed || !strings.Contains(res.Reason, "timed out") {
			t.Fatalf("res = %+v, want failed with a timeout reason", res)
		}
		assertConfigUntouched(t, before)
	})

	t.Run("cancelled", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		probes := make(chan sandbox.ShellCapability, 4)
		probes <- sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
		b := shellInstallBackend{}
		stubBackend(&b, probes, nil, time.Minute)
		b.run = func(ctx context.Context, winget string, argv []string) error {
			<-ctx.Done()
			return ctx.Err()
		}
		withStubbedBackend(t, b)
		app := NewApp()
		go func() {
			time.Sleep(20 * time.Millisecond)
			app.CancelShellInstall()
		}()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("cancelled must be structured, not a Go error: %v", err)
		}
		if res.Status != shellInstallStatusCancelled {
			t.Fatalf("status = %q, want cancelled", res.Status)
		}
		assertConfigUntouched(t, before)
		// Idempotent: cancelling with nothing running is a no-op.
		app.CancelShellInstall()
	})

	t.Run("post-install probe failure", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		probes := make(chan sandbox.ShellCapability, 4)
		probes <- sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
		probes <- sandbox.ShellCapability{ID: sandbox.ShellCapabilityGitBash}
		b := shellInstallBackend{}
		stubBackend(&b, probes, nil, time.Minute)
		withStubbedBackend(t, b)
		app := NewApp()
		res, err := app.InstallShellSupport("git-for-windows")
		if err != nil {
			t.Fatalf("undetected install must not error: %v", err)
		}
		if res.Status != shellInstallStatusFailed || !strings.Contains(res.Reason, "not detected") {
			t.Fatalf("res = %+v, want failed with a not-detected reason", res)
		}
		assertConfigUntouched(t, before)
	})

	t.Run("concurrent install is busy", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		before := readUserConfigOrEmpty(t)
		// Detection flips to available only once the (stubbed) install run
		// completes, so both racing callers pass the pre-probe.
		var probeMu sync.Mutex
		detected := false
		release := make(chan struct{})
		b := shellInstallBackend{
			platformSupported: func() bool { return true },
			probe: func(string) sandbox.ShellCapability {
				probeMu.Lock()
				defer probeMu.Unlock()
				return sandbox.ShellCapability{
					ID:        sandbox.ShellCapabilityGitBash,
					Available: detected,
					Path:      map[bool]string{true: `C:\Git\bin\bash.exe`}[detected],
				}
			},
			winget: func() string { return `C:\fake\winget.exe` },
			run: func(ctx context.Context, winget string, argv []string) error {
				<-release
				probeMu.Lock()
				detected = true
				probeMu.Unlock()
				return nil
			},
			timeout: time.Minute,
		}
		withStubbedBackend(t, b)
		app := NewApp()
		var wg sync.WaitGroup
		results := make([]ShellInstallResult, 2)
		for i := range 2 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i], _ = app.InstallShellSupport("git-for-windows")
			}(i)
		}
		// Wait until exactly one install owns the slot.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			app.shellInstall.mu.Lock()
			busy := app.shellInstall.cancel != nil
			app.shellInstall.mu.Unlock()
			if busy {
				break
			}
			time.Sleep(time.Millisecond)
		}
		close(release)
		wg.Wait()
		busyCount := 0
		installed := 0
		for _, res := range results {
			switch res.Status {
			case shellInstallStatusBusy:
				busyCount++
			case shellInstallStatusInstalled:
				installed++
			}
		}
		if busyCount != 1 || installed != 1 {
			t.Fatalf("concurrent installs: busy=%d installed=%d, want one of each (%+v)", busyCount, installed, results)
		}
		assertConfigUntouched(t, before)
	})
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
	if got := shellInstallActionViewForGOOS("darwin", false); got != nil {
		t.Fatalf("darwin action = %+v, want nil", got)
	}
	if got := shellInstallActionViewForGOOS("linux", true); got != nil {
		t.Fatalf("linux action = %+v, want nil", got)
	}
	got := shellInstallActionViewForGOOS("windows", true)
	if got == nil || got.Mode != "winget-user" || !got.Available {
		t.Fatalf("windows+winget action = %+v, want available winget-user", got)
	}
	manual := shellInstallActionViewForGOOS("windows", false)
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
	if runtime.GOOS == "windows" {
		if sb.ShellRepairGuidance != nil {
			t.Fatalf("repair guidance on Windows = %+v, want install action only", sb.ShellRepairGuidance)
		}
		if sb.GitRepairGuidance != nil {
			t.Fatalf("Git repair guidance on Windows = %+v, want Git for Windows action only", sb.GitRepairGuidance)
		}
	} else if runtime.GOOS == "darwin" {
		if sb.ShellRepairGuidance != nil {
			t.Fatalf("macOS shell guidance = %+v, want native zsh/sh fallback", sb.ShellRepairGuidance)
		}
		if sb.GitRepairGuidance == nil || sb.GitRepairGuidance.Command != "brew install git" {
			t.Fatalf("macOS Git guidance = %+v, want Homebrew Git command", sb.GitRepairGuidance)
		}
	} else {
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
