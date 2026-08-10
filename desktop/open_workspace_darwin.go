//go:build darwin

package main

import "os/exec"

func openWorkspacePath(path string) error {
	return exec.Command("open", path).Start()
}

func openWorkspacePathWithType(path string, _ bool) error {
	return openWorkspacePath(path)
}
