//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/proc"
	"reasonix/internal/sandbox"
)

// InstallGitBash on Linux checks package managers and installs bash/git.
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

	type pkgCmd struct {
		mgr string
		// steps is a sequence of commands to run. Each inner slice is one
		// exec invocation (program + args). This avoids passing shell
		// operators like "&&" as literal arguments to exec.Command.
		steps [][]string
	}
	candidates := []pkgCmd{
		{"apt-get", [][]string{
			{"apt-get", "update"},
			{"apt-get", "install", "-y", "bash", "git"},
		}},
		{"dnf", [][]string{
			{"dnf", "install", "-y", "bash", "git"},
		}},
		{"pacman", [][]string{
			{"pacman", "-S", "--noconfirm", "bash", "git"},
		}},
		{"zypper", [][]string{
			{"zypper", "install", "-y", "bash", "git"},
		}},
		{"apk", [][]string{
			{"apk", "add", "bash", "git"},
		}},
	}

	var found *pkgCmd
	for i := range candidates {
		if _, err := exec.LookPath(candidates[i].mgr); err == nil {
			found = &candidates[i]
			break
		}
	}

	if found == nil {
		return GitBashInstallResult{
			Success: false,
			Error:   "No supported package manager found (apt-get, dnf, pacman, zypper, apk). Please install bash and git manually.",
		}, fmt.Errorf("no package manager found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// If not running as root, prepend sudo to each step.
	needSudo := os.Getuid() != 0

	for _, step := range found.steps {
		var cmd *exec.Cmd
		if needSudo {
			cmd = proc.CommandContext(ctx, "sudo", step...)
		} else {
			cmd = proc.CommandContext(ctx, step[0], step[1:]...)
		}
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			outStr := strings.TrimSpace(string(out))
			if outStr == "" {
				outStr = runErr.Error()
			}
			return GitBashInstallResult{
				Success: false,
				Error:   fmt.Sprintf("Failed to install bash via %s: %s", found.mgr, outStr),
			}, runErr
		}
	}

	newSh := sandbox.ResolveShell("bash", "", nil)
	if newSh.Kind != sandbox.ShellBash || newSh.Path == "" {
		return GitBashInstallResult{
			Success: false,
			Error:   "Installation completed, but bash could not be located.",
		}, fmt.Errorf("bash not found after package install")
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
