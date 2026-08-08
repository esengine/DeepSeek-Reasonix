package config

import (
	"runtime"
	"testing"
)

func TestDesktopGlobalHotkeyDefaultAndOff(t *testing.T) {
	c := Default()
	got := c.DesktopGlobalHotkey()
	want := "ctrl+shift+space"
	if runtime.GOOS == "darwin" {
		want = "meta+shift+space"
	}
	if got != want {
		t.Fatalf("default = %q, want %q", got, want)
	}
	if err := c.SetDesktopGlobalHotkey("off"); err != nil {
		t.Fatal(err)
	}
	if got := c.DesktopGlobalHotkey(); got != "" {
		t.Fatalf("off = %q, want empty", got)
	}
	if got := c.DesktopGlobalHotkeySetting(); got != "off" {
		t.Fatalf("off setting = %q, want off", got)
	}
	if !c.DesktopGlobalHotkeyConfigured() {
		t.Fatal("expected configured sentinel")
	}
	if err := c.SetDesktopGlobalHotkey(""); err != nil {
		t.Fatal(err)
	}
	if c.DesktopGlobalHotkeyConfigured() {
		t.Fatal("expected cleared override")
	}
	if got := c.DesktopGlobalHotkey(); got != want {
		t.Fatalf("after clear = %q, want %q", got, want)
	}
}

func TestSetDesktopGlobalHotkeyNormalizes(t *testing.T) {
	c := Default()
	if err := c.SetDesktopGlobalHotkey("Ctrl + Shift + Space"); err != nil {
		t.Fatal(err)
	}
	if got := c.Desktop.GlobalHotkey; got != "ctrl+shift+space" {
		t.Fatalf("stored = %q", got)
	}
}
