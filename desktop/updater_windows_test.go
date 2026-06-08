//go:build windows

package main

import "testing"

func TestInstallerCommandPassesUnquotedDFlagLast(t *testing.T) {
	cmd := installerCommand(`C:\Temp\reasonix-update-1.exe`, `D:\Tools\Reasonix App`)
	if cmd.SysProcAttr == nil {
		t.Fatal("expected a raw command line forcing the install dir")
	}
	got := cmd.SysProcAttr.CmdLine
	want := `"C:\Temp\reasonix-update-1.exe" /D=D:\Tools\Reasonix App`
	if got != want {
		t.Fatalf("CmdLine = %q, want %q", got, want)
	}
}

func TestInstallerCommandWithoutDirSkipsDFlag(t *testing.T) {
	cmd := installerCommand(`C:\Temp\reasonix-update-1.exe`, "")
	if cmd.SysProcAttr != nil {
		t.Fatalf("no dir should leave NSIS InstallDir logic intact, got CmdLine %q", cmd.SysProcAttr.CmdLine)
	}
}
