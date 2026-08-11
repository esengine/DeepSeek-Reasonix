//go:build windows

package installsource

import (
	"context"
	"testing"
)

func TestPluginGitCommandHidesConsoleWindow(t *testing.T) {
	cmd := pluginGitCommand(context.Background(), "version")
	if cmd.SysProcAttr == nil {
		t.Fatal("plugin git command must set Windows process attributes")
	}
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("plugin git command can flash a console window: %+v", cmd.SysProcAttr)
	}
}
