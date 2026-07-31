// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

//go:build !windows

package middleware

import (
	"os"
)

// We ignore these errors as there could be handler that matches request path.
func isIgnorableOpenFileError(err error) bool {
	return os.IsNotExist(err)
}
