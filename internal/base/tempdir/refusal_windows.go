//go:build windows

package tempdir

import (
	"errors"
	"syscall"
)

// teardownRefusal reports the one removal failure that may be waited on. It is
// the one observed: a directory refusing to go while its content is settling.
// syscall.ENOTEMPTY is a different value here and never arrives, so the Windows
// code is what has to be named. Nothing wider without evidence -- a sharing or
// access refusal is a handle somebody still holds, and waiting hides it.
func teardownRefusal(err error) bool {
	return errors.Is(err, syscall.ERROR_DIR_NOT_EMPTY)
}
