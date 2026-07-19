//go:build windows

package sandbox

import (
	"reflect"
	"testing"
)

func TestWindowsSandboxIsUnavailable(t *testing.T) {
	if Available() {
		t.Fatal("Windows must report the retired OS-level Bash sandbox as unavailable")
	}
}

func TestWindowsCommandRemainsUnwrapped(t *testing.T) {
	sh := Shell{Kind: ShellPowerShell, Path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}
	argv, wrapped := Command(Spec{Mode: "enforce"}, sh, "Write-Output ok")
	if wrapped {
		t.Fatal("Windows command must not report an OS sandbox wrapper")
	}
	if len(argv) == 0 || argv[0] != sh.Path {
		t.Fatalf("command argv = %v, want shell path %q", argv, sh.Path)
	}
}

func TestWindowsCommandArgsRemainsUnwrapped(t *testing.T) {
	want := []string{"git", "status"}
	got, wrapped := CommandArgs(Spec{Mode: "enforce"}, append([]string(nil), want...))
	if wrapped {
		t.Fatal("Windows direct argv must not report an OS sandbox wrapper")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command argv = %v, want %v", got, want)
	}
}
