//go:build windows

package tempdir

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

// ENOTEMPTY is the obvious constant and the wrong one: Windows answers this
// refusal with its own code, so matching the portable name never fires and the
// wait would read as never needed.
func TestTheWindowsRefusalIsTheOneMatched(t *testing.T) {
	if !teardownRefusal(&os.PathError{Op: "remove", Path: "dir", Err: syscall.ERROR_DIR_NOT_EMPTY}) {
		t.Fatal("ERROR_DIR_NOT_EMPTY was not recognised")
	}
	if teardownRefusal(&os.PathError{Op: "remove", Path: "dir", Err: syscall.ENOTEMPTY}) {
		t.Fatal("ENOTEMPTY matched; it is a different value here and never arrives")
	}
	// A handle somebody still holds is not settling, and waiting hides it.
	if teardownRefusal(fmt.Errorf("wrapped: %w", syscall.ERROR_ACCESS_DENIED)) {
		t.Fatal("an access refusal was treated as teardown settling")
	}
}
