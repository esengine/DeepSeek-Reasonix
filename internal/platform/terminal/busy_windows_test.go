//go:build windows

package terminal

import "testing"

func TestBusyOnReadsTheConsoleNotTheParentChain(t *testing.T) {
	const console = 77
	tree := []proc{
		{10, "bash.exe", console}, {11, "bash.exe", console}, {12, "conhost.exe", 0},
		{20, "sleep.exe", 99},
	}
	if busyOn(10, tree) {
		t.Fatal("a bash behind its launcher reported busy while idle, or another console's process was counted")
	}
	tree = append(tree, proc{21, "sleep.exe", console})
	if !busyOn(10, tree) {
		t.Fatal("a command attached to the shell's console did not report busy")
	}
	if busyOn(99, tree) {
		t.Fatal("an unknown root reported busy")
	}
}
