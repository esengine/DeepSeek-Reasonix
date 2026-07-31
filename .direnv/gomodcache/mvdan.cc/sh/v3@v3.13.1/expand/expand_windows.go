// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package expand

import (
	"errors"
	"os"
	"syscall"
)

func isWindowsErrPathNotFound(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr) && pathErr.Err == syscall.ERROR_PATH_NOT_FOUND
}
