//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/proc"
	"reasonix/internal/sandbox"
)

// InstallGitBash installs Git for Windows via winget and activates it in settings.
func (a *App) InstallGitBash() (GitBashInstallResult, error) {
	// First check if a working Git Bash is already available
	sh := sandbox.ResolveShell("bash", "", nil)
	if sh.Kind == sandbox.ShellBash && strings.HasSuffix(strings.ToLower(sh.Path), "bash.exe") {
		_ = a.applyConfigChange(func(c *config.Config) error {
			c.Tools.Shell.Prefer = "bash"
			return nil
		})
		return GitBashInstallResult{
			Success: true,
			Path:    sh.Path,
			Message: "Git Bash is already installed.",
		}, nil
	}

	wingetPath, err := exec.LookPath("winget")
	if err != nil {
		return GitBashInstallResult{
			Success: false,
			Error:   "winget is not available on this system. Please install Git manually from https://git-scm.com/download/win",
		}, fmt.Errorf("winget not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := proc.CommandContext(ctx, wingetPath, "install", "--id", "Git.Git", "-e", "--source", "winget", "--accept-source-agreements", "--accept-package-agreements", "--silent")
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		outStr := strings.TrimSpace(string(out))
		if outStr == "" {
			outStr = runErr.Error()
		}
		return GitBashInstallResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to install Git via winget: %s", outStr),
		}, runErr
	}

	// Re-resolve shell after installation
	newSh := sandbox.ResolveShell("bash", "", nil)
	if newSh.Kind != sandbox.ShellBash {
		return GitBashInstallResult{
			Success: false,
			Error:   "Git installation completed, but Git Bash could not be located. You may need to restart Reasonix.",
		}, fmt.Errorf("git bash not found after installation")
	}

	_ = a.applyConfigChange(func(c *config.Config) error {
		c.Tools.Shell.Prefer = "bash"
		return nil
	})

	return GitBashInstallResult{
		Success: true,
		Path:    newSh.Path,
		Message: "Git Bash installed and activated successfully.",
	}, nil
}
