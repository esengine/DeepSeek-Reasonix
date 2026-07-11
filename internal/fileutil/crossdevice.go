package fileutil

import "strings"

// IsCrossDeviceError reports whether err is a rename/link failure caused by the
// source and destination residing on different filesystems (EXDEV, or Windows
// ERROR_NOT_SAME_DEVICE). It first consults the platform errno check
// (renameCrossesDevice, which unwraps *os.LinkError via errors.Is), then falls
// back to matching the error text so a cross-device failure is still recognized
// after the errno is lost — e.g. an error reconstructed from a message across a
// process or serialization boundary.
//
// It is the single source of truth for cross-device detection: move_file's
// copy-and-remove fallback and the checkpoint restore's move-back both call it,
// so the recognized-string set can no longer drift between them.
func IsCrossDeviceError(err error) bool {
	if err == nil {
		return false
	}
	if renameCrossesDevice(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range crossDeviceStrings {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// crossDeviceStrings is the union of the phrases the two former ad-hoc checks
// matched, so unifying on this helper widens neither caller's recognition nor
// narrows it.
var crossDeviceStrings = []string{
	"cross-device",
	"cross device",
	"different device",
	"different disk", // matches "different disk" and "different disk drive"
	"not same device",
}
