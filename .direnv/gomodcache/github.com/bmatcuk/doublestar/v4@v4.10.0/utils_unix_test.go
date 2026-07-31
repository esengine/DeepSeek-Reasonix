//go:build !windows

package doublestar

func mkdirpHidden(parts ...string) string {
	partsLen := len(parts)
	if partsLen > 0 {
		dirs := make([]string, partsLen)
		copy(dirs, parts)
		dirs[partsLen-1] = "." + dirs[partsLen-1]
		return mkdirp(dirs...)
	}
	return mkdirp(parts...)
}

func touchHidden(parts ...string) string {
	partsLen := len(parts)
	if partsLen > 0 {
		fileparts := make([]string, partsLen)
		copy(fileparts, parts)
		fileparts[partsLen-1] = "." + fileparts[partsLen-1]
		return touch(fileparts...)
	}
	return touch(parts...)
}
