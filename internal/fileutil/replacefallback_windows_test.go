//go:build windows

package fileutil

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRenameCrossesDeviceClassifiesFilterDriverError(t *testing.T) {
	// The filter-driver failure (#2696) surfaces as ERROR_NOT_SAME_DEVICE
	// wrapped in a LinkError; it must route to the copy fallback. A sharing
	// violation is transient and must not — copying there truncates dest under
	// a concurrent reader.
	notSameDevice := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: windows.ERROR_NOT_SAME_DEVICE}
	if !renameCrossesDevice(notSameDevice) {
		t.Fatal("ERROR_NOT_SAME_DEVICE must be classified as cross-device")
	}
	sharing := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: windows.ERROR_SHARING_VIOLATION}
	if renameCrossesDevice(sharing) {
		t.Fatal("a sharing violation is transient and must not take the non-atomic copy fallback")
	}
	accessDenied := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: windows.ERROR_ACCESS_DENIED}
	if renameCrossesDevice(accessDenied) {
		t.Fatal("access denied is transient (AV/indexer) and must not take the non-atomic copy fallback")
	}
}
