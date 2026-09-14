package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// desktopCLIBinaryPath resolves the reasonix CLI binary shipped beside the
// desktop service (the upload source for same-platform remotes), falling back
// to a reasonix command on PATH. Empty when neither exists.
func desktopCLIBinaryPath() string {
	packagedName, commandName := desktopCLIBinaryNames(runtime.GOOS)
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, packagedName))
	}
	if found, err := exec.LookPath(commandName); err == nil {
		candidates = append(candidates, found)
	}
	for _, candidate := range candidates {
		st, err := os.Stat(candidate)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if runtime.GOOS != "windows" && st.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate
	}
	return ""
}

// desktopCLIBinaryNames maps the running GOOS to the packaged CLI asset name
// and its command name on PATH.
func desktopCLIBinaryNames(goos string) (packaged, command string) {
	if goos == "windows" {
		return "reasonix-cli.exe", "reasonix.exe"
	}
	return "reasonix", "reasonix"
}
