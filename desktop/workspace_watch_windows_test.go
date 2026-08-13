//go:build windows

package main

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/gitcmd"
)

const workspaceGitCreateNoWindow = 0x08000000

func TestWorkspaceGitProbesHideConsoleWindowOnWindows(t *testing.T) {
	// Both startup probes use the same gitcmd.Command construction path.
	// Assert HideWindow + CREATE_NO_WINDOW for each rev-parse flag.
	for _, flag := range []string{"--git-dir", "--git-common-dir"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := gitcmd.Command(ctx, t.TempDir(), "rev-parse", flag)
		cancel()
		if cmd.SysProcAttr == nil {
			t.Fatalf("%s: SysProcAttr is nil", flag)
		}
		if !cmd.SysProcAttr.HideWindow {
			t.Fatalf("%s: HideWindow is false", flag)
		}
		if cmd.SysProcAttr.CreationFlags&workspaceGitCreateNoWindow == 0 {
			t.Fatalf("%s: CREATE_NO_WINDOW not set; CreationFlags=%#x", flag, cmd.SysProcAttr.CreationFlags)
		}
	}
}
