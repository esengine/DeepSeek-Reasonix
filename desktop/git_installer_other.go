//go:build !windows && !darwin && !linux

package main

import "fmt"

// InstallGitBash is a stub for other unsupported platforms.
func (a *App) InstallGitBash() (GitBashInstallResult, error) {
	return GitBashInstallResult{
		Success: false,
		Error:   "Automatic bash installation is not supported on this platform.",
	}, fmt.Errorf("platform not supported")
}
