//go:build windows

package proc

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestSameProcessIdentityUsesCreationTime(t *testing.T) {
	recorded := processRecord{
		pid:      123,
		exe:      "python.exe",
		created:  windows.Filetime{LowDateTime: 1, HighDateTime: 2},
		hasTimes: true,
	}
	current := recorded
	if !sameProcessIdentity(recorded, current) {
		t.Fatal("same process record should match")
	}
	current.created.LowDateTime++
	if sameProcessIdentity(recorded, current) {
		t.Fatal("same pid/exe with different creation time should not match")
	}
}

func TestSameProcessIdentityFallsBackToExecutableName(t *testing.T) {
	recorded := processRecord{pid: 123, exe: "Git-Bash.exe"}
	current := processRecord{pid: 123, exe: "git-bash.exe"}
	if !sameProcessIdentity(recorded, current) {
		t.Fatal("same pid/exe should match when creation times are unavailable")
	}
	current.exe = "powershell.exe"
	if sameProcessIdentity(recorded, current) {
		t.Fatal("different executable names should not match when creation times are unavailable")
	}
}
