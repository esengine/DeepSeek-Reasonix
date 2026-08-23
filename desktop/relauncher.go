package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/installlayout"
	"reasonix/internal/proc"
)

// relaunchThroughLauncher starts the permanent thin launcher (or falls back to
// the running executable). A legacy Guard binary is considered only as a
// one-release migration fallback for flat 1.18-1.19.1 installations.
func relaunchThroughLauncher() error {
	return relaunchThroughLauncherWithEnv(nil)
}

func relaunchThroughLauncherWithEnv(overrides map[string]string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	root := filepath.Dir(exe)
	if resolved, err := installlayout.ResolveInstallRoot(exe); err == nil && resolved != "" {
		root = resolved
	}
	candidates := []string{
		filepath.Join(root, "reasonix-launcher"),
		filepath.Join(root, "Reasonix.exe"),
		filepath.Join(root, "reasonix-guard"), // migration window only
	}
	if runtime.GOOS == "windows" {
		candidates[0] += ".exe"
		candidates[2] += ".exe"
	}
	launcher := exe
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			launcher = path
			break
		}
	}
	args := []string{}
	// Only legacy guard understands "launch --detach"; the thin launcher strips it.
	if strings.Contains(strings.ToLower(filepath.Base(launcher)), "guard") {
		args = []string{"launch", "--detach"}
	}
	cmd := proc.VisibleCommand(launcher, args...)
	if len(overrides) > 0 {
		cmd.Env = processEnvWithOverrides(os.Environ(), overrides)
	}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Start()
}

func processEnvWithOverrides(base []string, overrides map[string]string) []string {
	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, overridden := overrides[key]; overridden {
				continue
			}
		}
		env = append(env, entry)
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}
