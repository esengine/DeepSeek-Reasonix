//go:build !darwin && !windows

package main

import (
	"os/exec"
)

func openTerminal(path string) error {
	// x-terminal-emulator is the Debian/Ubuntu standard alternative.
	// It accepts no working-directory flag itself; the wrapped terminal
	// inherits the cwd from this process, so we chdir first.
	if _, err := exec.LookPath("x-terminal-emulator"); err == nil {
		cmd := exec.Command("x-terminal-emulator")
		cmd.Dir = path
		return cmd.Start()
	}

	// Common terminals with known working-directory flags.
	type termEntry struct {
		binary string
		args   []string
	}
	terminals := []termEntry{
		{"gnome-terminal", []string{"--working-directory=" + path}},
		{"konsole", []string{"--workdir", path}},
		{"xfce4-terminal", []string{"--working-directory=" + path}},
		{"mate-terminal", []string{"--working-directory=" + path}},
		{"lxterminal", []string{"--working-directory=" + path}},
		{"termite", []string{"-d", path}},
		{"alacritty", []string{"--working-directory", path}},
		{"kitty", []string{"--directory", path}},
		{"foot", []string{"--working-directory", path}},
		{"urxvt", []string{}},
		{"rxvt", []string{}},
		{"xterm", []string{}},
	}
	for _, term := range terminals {
		if _, err := exec.LookPath(term.binary); err == nil {
			cmd := exec.Command(term.binary, term.args...)
			cmd.Dir = path
			return cmd.Start()
		}
	}

	// Ultimate fallback: xterm with -e to cd and start a login shell.
	cmd := exec.Command("xterm", "-e", "/bin/sh", "-c", "cd "+escapeShellArg(path)+" && exec $SHELL")
	return cmd.Start()
}

func escapeShellArg(s string) string {
	// Minimal shell-safe quoting: wrap in single quotes, escape embedded quotes.
	// This is only used in the xterm fallback path.
	out := "'"
	for _, r := range s {
		if r == '\'' {
			out += "'\\''"
		} else {
			out += string(r)
		}
	}
	return out + "'"
}
