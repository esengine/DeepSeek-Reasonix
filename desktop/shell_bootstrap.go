package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"reasonix/internal/installlayout"
	"reasonix/internal/proc"
)

const shellServiceEnv = "REASONIX_DESKTOP_SERVICE"

// exitIfShellBootstrapped hands a plain desktop launch to the Electron shell
// installed beside this binary and exits; the shell restarts this binary as
// its --host-rpc service.
func exitIfShellBootstrapped(args []string) {
	if handled, exitCode := maybeBootstrapShell(args); handled {
		os.Exit(exitCode)
	}
}

// maybeBootstrapShell reports handled=false when the launch is already a
// service or contract mode, or when no shell executable is installed beside
// this binary.
func maybeBootstrapShell(args []string) (handled bool, exitCode int) {
	if hostRPCRequested(args) {
		return false, 0
	}
	if _, ok := emitContractDir(args); ok {
		return false, 0
	}
	exe, err := os.Executable()
	if err != nil {
		return false, 0
	}
	return bootstrapShell(exe, runtime.GOOS, args, os.Environ())
}

func bootstrapShell(exe, goos string, args, env []string) (handled bool, exitCode int) {
	shell, ok := shellExecutableBeside(exe, goos)
	if !ok {
		return false, 0
	}
	cmd := proc.VisibleCommand(shell, args...)
	cmd.Env = processEnvWithOverrides(env, map[string]string{shellServiceEnv: exe})
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	detachShellProcess(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start desktop shell:", err)
		return true, 1
	}
	return true, 0
}

// shellExecutableBeside resolves app/<shell> next to exe, or Contents/MacOS/
// Reasonix beside the service inside a macOS bundle, and never exe itself.
func shellExecutableBeside(exe, goos string) (string, bool) {
	exe = filepath.Clean(exe)
	shell := shellPathForExecutable(exe, goos)
	info, err := os.Lstat(shell)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	if same, err := installlayout.SameRegularFile(exe, shell); err != nil || same {
		return "", false
	}
	return shell, true
}

func shellPathForExecutable(exe, goos string) string {
	dir := filepath.Dir(exe)
	name := installlayout.ShellExecutableNameFor(goos)
	shell := filepath.Join(dir, installlayout.AppShellDirName, name)
	if goos == "darwin" {
		shell = filepath.Join(filepath.Dir(dir), "MacOS", name)
	} else if goos == "linux" && dir == "/usr/bin" {
		// The native package keeps executables in /usr/bin and Chromium in /usr/lib.
		shell = filepath.Join("/usr/lib/reasonix", installlayout.AppShellDirName, name)
	}
	return shell
}
