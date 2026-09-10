//go:build windows

package main

import "os/exec"

// detachShellProcess is a no-op: a GUI child outlives its parent on Windows.
func detachShellProcess(*exec.Cmd) {}
