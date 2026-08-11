//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

func waitForRemoteWindowOwnerExit(ctx context.Context, pid int) bool {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !remoteWindowOwnerAlive(pid) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func remoteWindowOwnerAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
