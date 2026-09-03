package pty

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ShellDriver provides shell-specific command wrapping and sentinel parsing.
type ShellDriver interface {
	Name() string
	IsShell() bool
	FormatCommand(cmd string, marker string) string
	ParseSentinel(line string, marker string) (exitCode int, ok bool)
}

// PosixShellDriver formats and parses completion sentinels for POSIX shells (bash, zsh, sh).
type PosixShellDriver struct{}

func (d *PosixShellDriver) Name() string { return "posix" }

func (d *PosixShellDriver) IsShell() bool { return true }

func (d *PosixShellDriver) FormatCommand(cmd string, marker string) string {
	sentinel := fmt.Sprintf("printf '\\n%s:%%s\\n' \"$?\"", marker)
	return cmd + "\n" + sentinel + "\n"
}

func (d *PosixShellDriver) ParseSentinel(line string, marker string) (int, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, marker) {
		return 0, false
	}
	suffix := trimmed[len(marker):]
	if len(suffix) < 2 || suffix[0] != ':' {
		return 0, false
	}
	code, err := strconv.Atoi(suffix[1:])
	if err != nil {
		return 0, false
	}
	return code, true
}

// RawDriver is used for interactive REPLs and non-shell processes (Python, node, gdb).
// It does not inject POSIX sentinels.
type RawDriver struct{}

func (d *RawDriver) Name() string { return "raw" }

func (d *RawDriver) IsShell() bool { return false }

func (d *RawDriver) FormatCommand(cmd string, marker string) string {
	return cmd + "\n"
}

func (d *RawDriver) ParseSentinel(line string, marker string) (int, bool) {
	return 0, false
}

// DetectDriver inspects the executable path and chooses the appropriate ShellDriver.
func DetectDriver(cmdPath string) ShellDriver {
	base := strings.ToLower(filepath.Base(cmdPath))
	switch base {
	case "bash", "zsh", "sh", "dash", "ksh", "csh", "tcsh":
		return &PosixShellDriver{}
	default:
		return &RawDriver{}
	}
}
