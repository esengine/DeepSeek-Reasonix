//go:build !linux && !darwin && !windows

package builtin

import "os"

func openedPath(*os.File) (string, bool) { return "", false }
