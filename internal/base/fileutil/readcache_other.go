//go:build !linux && !darwin && !freebsd && !netbsd

package fileutil

import "os"

func cacheFileIdentity(os.FileInfo) (fileIdentity, bool) { return fileIdentity{}, false }
