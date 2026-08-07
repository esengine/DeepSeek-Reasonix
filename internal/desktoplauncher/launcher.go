// Package desktoplauncher implements the permanent Reasonix desktop entry
// point. It deliberately owns no crash-loop, rollback, or safe-mode policy.
package desktoplauncher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/installlayout"
)

// Run resolves the active desktop, performs the one-time legacy handoff when
// needed, and starts the desktop process.
func Run(args []string, buildVersion string) int {
	if len(args) == 1 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println("reasonix-launcher", buildVersion)
			return 0
		case "help", "--help", "-h":
			usage()
			return 0
		}
	}

	installRoot, err := ResolveInstallRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := runLegacyMigratorIfNeeded(installRoot); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	desktopPath, err := ResolveDesktopPath(installRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	cmd := exec.Command(desktopPath, StripLegacyLaunchArgs(args)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Dir = installRoot
	if DetachByDefault() {
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 1
		}
		return 0
	}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// ResolveInstallRoot returns the directory containing the launcher.
func ResolveInstallRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve launcher path: %w", err)
	}
	return resolveInstallRoot(exe)
}

func resolveInstallRoot(exe string) (string, error) {
	resolved, err := resolveExecutablePath(exe)
	if err != nil {
		return "", fmt.Errorf("resolve launcher path: %w", err)
	}
	return filepath.Clean(filepath.Dir(resolved)), nil
}

// ResolveDesktopPath resolves current.json when present. A present but invalid
// pointer is always fatal; only a genuinely absent pointer may use the flat
// sibling fallback needed by package-managed installs.
func ResolveDesktopPath(installRoot string) (string, error) {
	currentPath := filepath.Join(installRoot, installlayout.CurrentFileName)
	if _, err := os.Lstat(currentPath); err == nil {
		return installlayout.ActiveDesktopPath(installRoot)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect current.json: %w", err)
	}
	if path := siblingDesktop(installRoot); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("cannot locate reasonix-desktop under %s (missing current.json)", installRoot)
}

func runLegacyMigratorIfNeeded(installRoot string) error {
	currentPath := filepath.Join(installRoot, installlayout.CurrentFileName)
	if _, err := os.Lstat(currentPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect current.json before migration: %w", err)
	}

	migratorName := "reasonix-guard"
	if runtime.GOOS == "windows" {
		migratorName += ".exe"
	}
	migratorPath := filepath.Join(installRoot, migratorName)
	info, err := os.Lstat(migratorPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect legacy migrator: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("legacy migrator is not a regular file")
	}

	cmd := exec.Command(migratorPath, "--install-root", installRoot, "--no-relaunch")
	cmd.Dir = installRoot
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("legacy layout migration failed: %w", err)
	}
	if _, err := installlayout.ReadCurrent(installRoot); err != nil {
		return fmt.Errorf("legacy layout migration did not commit current.json: %w", err)
	}
	// The parent launcher owns Windows cleanup after the migrator process exits.
	// Unix migrators normally unlink themselves, so NotExist is also success.
	if err := os.Remove(migratorPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove completed legacy migrator: %w", err)
	}
	return nil
}

func siblingDesktop(installRoot string) string {
	path := filepath.Join(installRoot, installlayout.DesktopBinaryName())
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return path
}

// StripLegacyLaunchArgs removes tokens emitted by old Guard shortcuts.
func StripLegacyLaunchArgs(args []string) []string {
	out := make([]string, 0, len(args))
	skipNext := false
	for i := 0; i < len(args); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		arg := args[i]
		switch arg {
		case "launch", "--detach", "--safe-mode", "-safe-mode":
			continue
		case "--app":
			skipNext = true
			continue
		case "--":
			return append(out, args[i:]...)
		}
		if strings.HasPrefix(arg, "--app=") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

// DetachByDefault reports whether the Windows packaged entry should start the
// desktop and exit immediately.
func DetachByDefault() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	name := strings.ToLower(filepath.Base(exe))
	return name == "reasonix-launcher.exe" || name == "reasonix.exe"
}

func usage() {
	fmt.Println("usage: reasonix-launcher [args...]")
	fmt.Println("  Starts the active Reasonix desktop from current.json.")
	fmt.Println("  Legacy --safe-mode / launch --detach tokens are ignored.")
}
