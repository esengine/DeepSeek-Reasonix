//go:build !windows

package proc

import "os/exec"

type TreeTracker struct{}

func TrackTree(*exec.Cmd) *TreeTracker { return nil }

func (*TreeTracker) Stop() {}

func (*TreeTracker) Kill() int { return 0 }
