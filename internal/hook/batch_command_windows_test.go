//go:build windows

package hook

import (
	"context"
	"testing"
)

func TestNewWindowsBatchCommandPreservesBackgroundPolicy(t *testing.T) {
	const commandLine = `cmd.exe /d /s /c ""C:\Program Files\tool.cmd" run"`
	cmd, ok := newWindowsBatchCommand(context.Background(), commandLine, true)
	if !ok || cmd == nil {
		t.Fatal("expected a Windows batch command")
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatal("batch command lost the hidden-window policy")
	}
	if cmd.SysProcAttr.CmdLine != commandLine {
		t.Fatalf("CmdLine = %q, want %q", cmd.SysProcAttr.CmdLine, commandLine)
	}
}
