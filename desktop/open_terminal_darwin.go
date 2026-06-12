//go:build darwin

package main

import "os/exec"

func openTerminal(path string) error {
	return exec.Command("open", "-a", "Terminal", path).Start()
}
