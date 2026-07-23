package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type remoteSSHLookPath func(string) (string, error)
type remoteSSHStat func(string) (os.FileInfo, error)

// resolveRemoteSSHExecutable keeps configured overrides unchanged. For the
// default Windows path it prefers the inbox OpenSSH client in System32 because
// GUI applications do not reliably inherit the system PATH. Other platforms
// retain normal PATH lookup through exec.CommandContext.
func resolveRemoteSSHExecutable(configured string) (string, error) {
	return resolveRemoteSSHExecutableForOS(
		runtime.GOOS,
		configured,
		[]string{os.Getenv("SystemRoot"), os.Getenv("WINDIR")},
		exec.LookPath,
		os.Stat,
	)
}

func resolveRemoteSSHExecutableForOS(
	goos string,
	configured string,
	windowsRoots []string,
	lookPath remoteSSHLookPath,
	stat remoteSSHStat,
) (string, error) {
	configured = strings.TrimSpace(configured)
	if strings.IndexByte(configured, 0) >= 0 {
		return "", errors.New("invalid OpenSSH executable path")
	}
	if configured != "" {
		return configured, nil
	}
	if goos != "windows" {
		return "ssh", nil
	}

	seen := make(map[string]struct{}, len(windowsRoots))
	for _, root := range windowsRoots {
		root = strings.TrimSpace(root)
		if root == "" || strings.IndexByte(root, 0) >= 0 || !filepath.IsAbs(root) {
			continue
		}
		root = filepath.Clean(root)
		key := strings.ToLower(root)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		candidate := filepath.Join(root, "System32", "OpenSSH", "ssh.exe")
		info, err := stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}

	if lookPath != nil {
		if resolved, err := lookPath("ssh.exe"); err == nil {
			resolved = strings.TrimSpace(resolved)
			if resolved != "" && strings.IndexByte(resolved, 0) < 0 && filepath.IsAbs(resolved) {
				return filepath.Clean(resolved), nil
			}
		}
	}

	// Preserve the existing controlled CLI_NOT_FOUND classification at Cmd.Start
	// when neither the inbox location nor PATH contains OpenSSH.
	return "ssh.exe", nil
}
