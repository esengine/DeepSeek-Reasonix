//go:build !windows && !darwin && !linux && !freebsd && !netbsd && !openbsd && !dragonfly

package isolation

import "os"

func candidateDirectoryID(*os.File) (string, error) { return "", errUnavailable }
