//go:build darwin

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

// InstallGitBash on macOS ensures bash/git is available, installing via Homebrew if needed.
func (a *App) InstallGitBash() (GitBashInstallResult, error) {
	sh := sandbox.ResolveShell("bash", "", nil)
	if sh.Kind == sandbox.ShellBash && sh.Path != "" {
		_ = a.applyConfigChange(func(c *config.Config) error {
			c.Tools.Shell.Prefer = "bash"
			return nil
		})
		return GitBashInstallResult{
			Success: true,
			Path:    sh.Path,
			Message: fmt.Sprintf("Bash is available at %s.", sh.Path),
		}, nil
	}

	brewPath, err := exec.LookPath("brew")
	if err != nil {
		return GitBashInstallResult{
			Success: false,
			Error:   "Homebrew (brew) is not found. Please install Homebrew or Git from https://brew.sh",
		}, fmt.Errorf("brew not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := proc.CommandContext(ctx, brewPath, "install", "bash", "git")
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		outStr := strings.TrimSpace(string(out))
		if outStr == "" {
			outStr = runErr.Error()
		}
		return GitBashInstallResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to install bash via brew: %s", outStr),
		}, runErr
	}

	newSh := sandbox.ResolveShell("bash", "", nil)
	if newSh.Kind != sandbox.ShellBash || newSh.Path == "" {
		return GitBashInstallResult{
			Success: false,
			Error:   "Installation completed, but bash could not be located.",
		}, fmt.Errorf("bash not found after brew install")
	}

	_ = a.applyConfigChange(func(c *config.Config) error {
		c.Tools.Shell.Prefer = "bash"
		return nil
	})

	return GitBashInstallResult{
		Success: true,
		Path:    newSh.Path,
		Message: "Bash installed and activated successfully.",
	}, nil
}
