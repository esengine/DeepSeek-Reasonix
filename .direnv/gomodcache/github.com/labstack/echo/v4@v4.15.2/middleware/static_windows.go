// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"os"
)

// We ignore these errors as there could be handler that matches request path.
//
// As of Go 1.20 filepath.Clean has different behaviour on OS related filesystems so we need to use path.Clean
// on Windows which has some caveats. The Open methods might return different errors than earlier versions and
// as of 1.20 path checks are more strict on the provided path and considers [UNC](https://en.wikipedia.org/wiki/Path_(computing)#UNC)
// paths with missing host etc parts as invalid. Previously it would result you `fs.ErrNotExist`.
//
// For 1.20@Windows we need to treat those errors the same as `fs.ErrNotExists` so we can continue handling
// errors in the middleware/handler chain. Otherwise we might end up with status 500 instead of finding a route
// or return 404 not found.
func isIgnorableOpenFileError(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	errTxt := err.Error()
	return errTxt == "http: invalid or unsafe file path" || errTxt == "invalid path"
}
