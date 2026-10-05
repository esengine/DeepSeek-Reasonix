//go:build windows

package builtin

import (
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// localizedErrno carries the code Windows returned behind a message in another
// language, which is what a non-English install actually produces.
type localizedErrno struct{ errno syscall.Errno }

func (e localizedErrno) Error() string {
	return "系统无法将文件移到不同的磁盘驱动器。"
}
func (e localizedErrno) Unwrap() error { return e.errno }

func TestCrossDeviceMoveIsReadFromTheCodeNotTheMessage(t *testing.T) {
	localized := &os.LinkError{Op: "rename", Err: localizedErrno{windows.ERROR_NOT_SAME_DEVICE}}
	if !isCrossDeviceMove(localized) {
		t.Fatal("a cross-volume rename went unrecognized because its message was not English")
	}
	english := &os.LinkError{Op: "rename", Err: windows.ERROR_NOT_SAME_DEVICE}
	if !isCrossDeviceMove(english) {
		t.Fatal("ERROR_NOT_SAME_DEVICE was not recognized")
	}
	other := &os.LinkError{Op: "rename", Err: windows.ERROR_ACCESS_DENIED}
	if isCrossDeviceMove(other) {
		t.Fatal("an unrelated failure was treated as cross-volume")
	}
}
