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

func TestSameProcessIdentityFailsClosedWhenOnlyOneCreationTimeIsAvailable(t *testing.T) {
	recorded := processRecord{
		pid: 123, created: windows.Filetime{LowDateTime: 1}, hasTimes: true,
	}
	current := processRecord{pid: 123}
	if sameProcessIdentity(recorded, current) {
		t.Fatal("missing current creation time was accepted as the tracked process identity")
	}
}

func TestTreeTrackerDoesNotReplaceReusedRootIdentity(t *testing.T) {
	originalRoot := processRecord{
		pid: 41, created: windows.Filetime{LowDateTime: 1}, hasTimes: true,
	}
	reusedRoot := processRecord{
		pid: 41, created: windows.Filetime{LowDateTime: 2}, hasTimes: true,
	}
	reusedChild := processRecord{
		pid: 42, parent: 41, created: windows.Filetime{LowDateTime: 3}, hasTimes: true,
	}
	tracker := &TreeTracker{
		root:    41,
		done:    make(chan struct{}),
		records: map[uint32]processRecord{41: originalRoot},
	}

	tracker.recordSnapshot(map[uint32]processRecord{41: reusedRoot, 42: reusedChild})
	records := tracker.snapshot()
	if len(records) != 1 || records[0].created != originalRoot.created {
		t.Fatalf("reused root replaced immutable tracker identity: %#v", records)
	}
}

func TestTreeTrackerDoesNotReplaceReusedDescendantIdentity(t *testing.T) {
	root := processRecord{
		pid: 41, created: windows.Filetime{LowDateTime: 1}, hasTimes: true,
	}
	originalChild := processRecord{
		pid: 42, parent: 41, created: windows.Filetime{LowDateTime: 2}, hasTimes: true,
	}
	reusedChild := processRecord{
		pid: 42, parent: 41, created: windows.Filetime{LowDateTime: 3}, hasTimes: true,
	}
	tracker := &TreeTracker{
		root: 41,
		done: make(chan struct{}),
		records: map[uint32]processRecord{
			41: root,
			42: originalChild,
		},
	}

	tracker.recordSnapshot(map[uint32]processRecord{41: root, 42: reusedChild})
	records := tracker.snapshot()
	if len(records) != 2 {
		t.Fatalf("tracker records = %#v, want root and original child", records)
	}
	for _, record := range records {
		if record.pid == 42 && record.created != originalChild.created {
			t.Fatalf("reused descendant replaced immutable identity: %#v", record)
		}
	}
}

func TestTreeTrackerAddsNewDescendantWhileOriginalRootLives(t *testing.T) {
	root := processRecord{
		pid: 41, created: windows.Filetime{LowDateTime: 1}, hasTimes: true,
	}
	child := processRecord{
		pid: 42, parent: 41, created: windows.Filetime{LowDateTime: 2}, hasTimes: true,
	}
	tracker := &TreeTracker{
		root:    41,
		done:    make(chan struct{}),
		records: map[uint32]processRecord{41: root},
	}

	tracker.recordSnapshot(map[uint32]processRecord{41: root, 42: child})
	records := tracker.snapshot()
	if len(records) != 2 {
		t.Fatalf("live descendant was not recorded: %#v", records)
	}
}

func TestTreeTrackerFrozenIdentitySetRejectsLaterRecords(t *testing.T) {
	root := processRecord{
		pid: 41, created: windows.Filetime{LowDateTime: 1}, hasTimes: true,
	}
	child := processRecord{
		pid: 42, parent: 41, created: windows.Filetime{LowDateTime: 2}, hasTimes: true,
	}
	tracker := &TreeTracker{
		root:    41,
		done:    make(chan struct{}),
		records: map[uint32]processRecord{41: root},
		frozen:  true,
	}

	tracker.recordSnapshot(map[uint32]processRecord{41: root, 42: child})
	if records := tracker.snapshot(); len(records) != 1 {
		t.Fatalf("frozen tracker accepted a later PID identity: %#v", records)
	}
}
