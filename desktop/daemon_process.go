package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/config"
	daemonapi "reasonix/internal/daemon"
)

type DaemonProcessActionResult struct {
	Message string           `json:"message,omitempty"`
	Status  DaemonStatusView `json:"status"`
}

type DaemonStartupHelperView struct {
	Platform         string `json:"platform"`
	InstallCommand   string `json:"installCommand"`
	UninstallCommand string `json:"uninstallCommand"`
	PrintCommand     string `json:"printCommand"`
	Description      string `json:"description"`
}

func (a *App) StartDaemon(addr string) (DaemonProcessActionResult, error) {
	if status := a.DaemonStatus(addr); status.Connected {
		return DaemonProcessActionResult{Message: "daemon already running", Status: status}, nil
	}
	sessionDir := config.SessionDir()
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return DaemonProcessActionResult{Status: a.DaemonStatus(addr)}, err
	}
	exe, err := daemonCLIPath()
	if err != nil {
		return DaemonProcessActionResult{Status: a.DaemonStatus(addr)}, fmt.Errorf("reasonix CLI not found in PATH; install it or run `reasonix daemon start` manually")
	}
	logPath := daemonapi.LogFile(sessionDir)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return DaemonProcessActionResult{Status: a.DaemonStatus(addr)}, fmt.Errorf("open daemon log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "daemon", "start", "--addr", normalizeDaemonAddr(addr), "--dir", sessionDir, "--log-file", logPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return DaemonProcessActionResult{Status: a.DaemonStatus(addr)}, err
	}
	status := a.waitDaemonStatus(addr, true, 5*time.Second)
	if !status.Connected {
		if status.Error != "" {
			return DaemonProcessActionResult{Status: status}, fmt.Errorf("daemon did not become reachable: %s", status.Error)
		}
		return DaemonProcessActionResult{Status: status}, fmt.Errorf("daemon did not become reachable")
	}
	return DaemonProcessActionResult{Message: "daemon started", Status: status}, nil
}

func (a *App) StopDaemon(addr string) (DaemonProcessActionResult, error) {
	status := a.DaemonStatus(addr)
	if !status.Connected {
		return DaemonProcessActionResult{Message: "daemon already offline", Status: status}, nil
	}
	if status.PID <= 0 {
		return DaemonProcessActionResult{Status: status}, fmt.Errorf("daemon status did not include a PID")
	}
	proc, err := os.FindProcess(status.PID)
	if err != nil {
		return DaemonProcessActionResult{Status: status}, err
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		if killErr := proc.Kill(); killErr != nil {
			return DaemonProcessActionResult{Status: status}, fmt.Errorf("interrupt failed: %v; kill failed: %w", err, killErr)
		}
	}
	next := a.waitDaemonStatus(addr, false, 5*time.Second)
	if next.Connected && next.PID == status.PID {
		return DaemonProcessActionResult{Status: next}, fmt.Errorf("daemon is still running with pid %d", status.PID)
	}
	return DaemonProcessActionResult{Message: "daemon stopped", Status: next}, nil
}

func (a *App) RestartDaemon(addr string) (DaemonProcessActionResult, error) {
	if result, err := a.StopDaemon(addr); err != nil && result.Status.Connected {
		return result, err
	}
	return a.StartDaemon(addr)
}

func (a *App) DaemonStartupHelper() DaemonStartupHelperView {
	return DaemonStartupHelperView{
		Platform:         desktopPlatformName(),
		InstallCommand:   "reasonix daemon startup install",
		UninstallCommand: "reasonix daemon startup uninstall",
		PrintCommand:     "reasonix daemon startup print",
		Description:      "Install a user-level login helper for the local Reasonix daemon.",
	}
}

func (a *App) waitDaemonStatus(addr string, wantConnected bool, timeout time.Duration) DaemonStatusView {
	deadline := time.Now().Add(timeout)
	var last DaemonStatusView
	for {
		last = a.DaemonStatus(addr)
		if last.Connected == wantConnected {
			return last
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func daemonCLIPath() (string, error) {
	if env := strings.TrimSpace(os.Getenv("REASONIX_CLI")); env != "" {
		return env, nil
	}
	if exe, err := exec.LookPath("reasonix"); err == nil {
		return exe, nil
	}
	if self, err := os.Executable(); err == nil {
		dir := filepath.Dir(self)
		for _, name := range []string{"reasonix", "reasonix.exe"} {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("reasonix CLI not found in PATH; set REASONIX_CLI or install the reasonix command")
}

func desktopPlatformName() string {
	switch runtime.GOOS {
	case "darwin":
		return "launchd"
	case "linux":
		return "systemd --user"
	case "windows":
		return "Windows Scheduled Task"
	default:
		return runtime.GOOS
	}
}
