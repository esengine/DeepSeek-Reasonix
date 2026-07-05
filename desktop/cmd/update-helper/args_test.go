package main

import (
	"strings"
	"testing"
)

func TestInstallerCommandLineIsSilentAndLeavesDFlagLast(t *testing.T) {
	got := installerCommandLine(`C:\Temp\Reasonix Installer.exe`, `D:\Tools\Reasonix App`)
	want := `"C:\Temp\Reasonix Installer.exe" /S /D=D:\Tools\Reasonix App`
	if got != want {
		t.Fatalf("installerCommandLine = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, `/D=D:\Tools\Reasonix App`) {
		t.Fatalf("/D= must be the final unquoted NSIS token, got %q", got)
	}
}
