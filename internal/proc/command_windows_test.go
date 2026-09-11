//go:build windows

package proc

import "testing"

func TestCommandIsHiddenAndVisibleCommandOptsOut(t *testing.T) {
	background := Command("cmd.exe", "/c", "exit", "0")
	if background.SysProcAttr == nil || !background.SysProcAttr.HideWindow {
		t.Fatal("background command is not hidden")
	}
	if background.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("background command does not set CREATE_NO_WINDOW")
	}

	visible := VisibleCommand("notepad.exe")
	if visible.SysProcAttr != nil && (visible.SysProcAttr.HideWindow || visible.SysProcAttr.CreationFlags&createNoWindow != 0) {
		t.Fatal("visible command unexpectedly suppresses its window")
	}
}
