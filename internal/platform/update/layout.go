package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/installlayout"
)

// Layout is where this build lives on disk. The three are resolved together
// because they are three answers to one question, and an installer that pairs
// a root from one install with a launcher from another writes a broken one.
type Layout struct {
	Executable string // the running binary, symlinks resolved
	Root       string // owns current.json, or the flat install directory
	Launcher   string // thin launcher to restart through; else Executable
}

// At resolves the layout of the build that executable belongs to. The caller
// states the executable because a shell whose process is a resource inside the
// application cannot be asked for it. Every field
// is empty when there is no executable to resolve from — a caller must treat
// that as "do not install", not as "install into the current directory".
func At(exe string, line Line) Layout {
	if strings.TrimSpace(exe) == "" {
		return Layout{}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	l := Layout{Executable: exe, Root: filepath.Dir(exe)}
	if root, err := installlayout.ResolveInstallRoot(exe); err == nil && root != "" {
		l.Root = root
	}
	l.Launcher = launcherIn(l.Root, exe, line.Launchers)
	return l
}

// Relaunch starts the launcher and returns once it has been started, not once
// it is up: the caller is about to exit so the new process can replace it.
func (l Layout) Relaunch() error {
	if l.Launcher == "" {
		return os.ErrNotExist
	}
	args := []string{}
	// Only the legacy Guard understands "launch --detach"; the thin launcher
	// strips it. Guard is a one-release migration fallback for flat 1.18-1.19.1.
	if strings.Contains(strings.ToLower(filepath.Base(l.Launcher)), "guard") {
		args = []string{"launch", "--detach"}
	}
	cmd := exec.Command(l.Launcher, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Start()
}

// Versioned reports whether this install carries the v1.20+ versioned layout,
// where a release is published into its own directory and current.json swaps
// last. A flat install predates that and needs the transactional apply path.
func (l Layout) Versioned() bool {
	return l.Root != "" && installlayout.HasCurrent(l.Root)
}

// launcherIn finds the entry point that survives the running binary being
// replaced. An install missing every candidate relaunches itself, which is
// correct for a portable copy that was never installed.
func launcherIn(root, exe string, candidates []string) string {
	for _, name := range candidates {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	// An incomplete install can still have a launcher beside the executable
	// when the resolved root pointed elsewhere.
	for _, name := range candidates {
		path := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return exe
}
