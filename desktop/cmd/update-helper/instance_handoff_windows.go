//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/desktop/internal/instanceidentity"
	"reasonix/internal/installlayout"
)

var (
	desktopEndpointImageFn = desktopEndpointImage
	handoffNowFn           = time.Now
	handoffSleepFn         = time.Sleep
	notifyHandoffBlockedFn = notifyHandoffBlocked
)

const desktopHandoffTimeout = 30 * time.Second

// verifyDesktopHandoff checks the single-instance owner, never all installed processes.
func verifyDesktopHandoff(installDir string, started bool) error {
	id := instanceidentity.UpdateID()
	home := os.Getenv("REASONIX_HOME")
	if !instanceidentity.Valid(id) || !filepath.IsAbs(home) || id != instanceidentity.ForHome(home) {
		return fmt.Errorf("update instance identity is unavailable; automatic restart cannot be verified")
	}
	target := filepath.Join(installDir, installlayout.DesktopBinaryName())
	if installlayout.HasCurrent(installDir) {
		var err error
		target, err = installlayout.ActiveDesktopPath(installDir)
		if err != nil {
			return err
		}
	}
	deadline := handoffNowFn().Add(desktopHandoffTimeout)
	for {
		path, err := desktopEndpointImageFn(id)
		if err != nil && !(started && errors.Is(err, errEndpointStarting)) {
			return fmt.Errorf("inspect update instance: %w", err)
		}
		if path != "" {
			same, err := installlayout.SameRegularFile(path, target)
			if err != nil {
				return fmt.Errorf("verify update instance image: %w", err)
			}
			if !same {
				return fmt.Errorf("another version still owns this data home's desktop endpoint")
			}
			return nil
		}
		if !started {
			return nil
		}
		if !handoffNowFn().Before(deadline) {
			return fmt.Errorf("new desktop did not acquire its instance endpoint within %s", desktopHandoffTimeout)
		}
		handoffSleepFn(100 * time.Millisecond)
	}
}
