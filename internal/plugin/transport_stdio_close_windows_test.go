//go:build windows

package plugin

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestStdioGracefulExitReleasesWindowsJobBeforeInstanceSlot(t *testing.T) {
	transport, err := newStdioTransport(context.Background(), Spec{
		Name: "graceful-exit", Command: "cmd.exe", Args: []string{"/c", "exit", "0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transport.close)
	if transport.job == 0 {
		t.Fatal("stdio process has no Windows job")
	}
	transport.wait()
	jobInfo := func() error {
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		return windows.QueryInformationJobObject(windows.Handle(transport.job), windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	}
	if err := jobInfo(); err != nil {
		t.Fatalf("job before close: %v", err)
	}
	released := false
	transport.releaseSlot = func() {
		released = true
		if err := jobInfo(); !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
			t.Errorf("instance slot released before Windows job handle: %v", err)
		}
	}
	transport.close()
	if !released {
		t.Fatal("instance slot was not released")
	}
}
