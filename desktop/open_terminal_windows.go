//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
)

func openTerminal(path string) error {
	// Prefer Windows Terminal if available.
	if _, err := exec.LookPath("wt.exe"); err == nil {
		return exec.Command("wt.exe", "-d", path).Start()
	}
	// Fall back to cmd.exe.
	cmd := "cmd.exe"
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = os.Getenv("windir")
	}
	if root != "" {
		cmd = filepath.Join(root, "system32", "cmd.exe")
	}
	return exec.Command(cmd, "/K", "cd /d", path).Start()
}
