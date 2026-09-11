//go:build windows

package desktoplauncher

import (
	"fmt"
	"os"
	"os/exec"
	"reasonix/internal/config"
	"reasonix/internal/desktopinstance"
	"reasonix/internal/installlayout"
	"reasonix/internal/proc"
)

func coordinatedLaunch(root string, args []string) (bool, int) {
	// Shell-less versioned layouts (pre-shell releases, partial rollbacks)
	// carry no shell lifecycle to coordinate or verify; launch directly.
	if !installlayout.HasCurrent(root) || !installlayout.HasActiveShell(root) {
		return false, 0
	}
	err := desktopinstance.LaunchAndVerify(root, config.ReasonixHomeDir(), os.Getenv("REASONIX_NONINTERACTIVE") != "1", func() error {
		path, err := ResolveDesktopPath(root)
		if err != nil {
			return err
		}
		cmd := exec.Command(path, StripLegacyLaunchArgs(args)...)
		proc.HideConsole(cmd)
		cmd.Dir = root
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}, StripLegacyLaunchArgs(args)...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if os.Getenv("REASONIX_NONINTERACTIVE") != "1" {
			desktopinstance.Notify(err)
		}
		return true, desktopinstance.ExitCode(err)
	}
	return true, 0
}
