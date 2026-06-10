// Package fileutil provides filesystem utilities beyond what the standard
// library offers: atomic file replacement, cross-platform path joining with
// Windows cross-drive safety, and related helpers.
package fileutil

import (
	"path/filepath"
)

// SafeJoin joins path elements like filepath.Join, but on Windows it correctly
// handles the case where a later element is an absolute path from a different
// drive letter. Go's filepath.Join (as of Go 1.24) does NOT reset the result
// when encountering an absolute path on Windows — it concatenates blindly:
//
//	filepath.Join("E:\\proj", "C:\\Users\\test") → "E:\\proj\\C:\\Users\\test"  (WRONG)
//
// SafeJoin walks elements from right to left. When it finds an absolute element
// it discards everything before it and joins from that point:
//
//	SafeJoin("E:\\proj", "C:\\Users\\test") → "C:\\Users\\test"  (CORRECT)
//
// On Unix this is a no-op wrapper since filepath.Join already has the correct
// behavior for that platform.
//
// See https://github.com/golang/go/issues/51619
// See https://github.com/esengine/DeepSeek-Reasonix/issues/3850
func SafeJoin(elem ...string) string {
	// Walk right to left; the rightmost absolute element anchors the result.
	absIdx := -1
	for i := len(elem) - 1; i >= 0; i-- {
		if filepath.IsAbs(elem[i]) {
			absIdx = i
			break
		}
	}
	if absIdx >= 0 {
		return filepath.Join(elem[absIdx:]...)
	}
	return filepath.Join(elem...)
}
