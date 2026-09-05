package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/installlayout"
	"reasonix/internal/proc"
)

// relaunchThroughLauncher starts the permanent thin launcher (or the active
// desktop). It never restarts a retained versions/<old>/ desktop binary.
func relaunchThroughLauncher() error {
	return relaunchThroughLauncherWithEnv(nil)
}

func relaunchThroughLauncherWithEnv(overrides map[string]string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	launcher, err := relaunchTarget(exe)
	if err != nil {
		return err
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

func relaunchTarget(exe string) (string, error) {
	root := filepath.Dir(exe)
	if resolved, err := installlayout.ResolveInstallRoot(exe); err == nil && resolved != "" {
		root = resolved
	}
	path, err := installlayout.StableRelaunchPath(root)
	if err != nil {
		return "", err
	}
	return path, nil
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
