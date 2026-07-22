//go:build !windows

package sandbox

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func inspectCapabilityDevice(path string) (CapabilityDevice, error) {
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return CapabilityDevice{}, err
	}
	return capabilityDeviceFromStat(path, uint32(stat.Mode), uint64(stat.Rdev))
}

func capabilityDeviceFromStat(path string, mode uint32, rdev uint64) (CapabilityDevice, error) {
	var kind CapabilityDeviceKind
	switch mode & unix.S_IFMT {
	case unix.S_IFCHR:
		kind = CapabilityCharacterDevice
	case unix.S_IFBLK:
		kind = CapabilityBlockDevice
	case unix.S_IFLNK:
		return CapabilityDevice{}, fmt.Errorf("target must not be a symlink")
	default:
		return CapabilityDevice{}, fmt.Errorf("target must be a character or block device")
	}
	return CapabilityDevice{
		Path:      path,
		Canonical: filepath.Clean(path),
		Kind:      kind,
		Major:     unix.Major(rdev),
		Minor:     unix.Minor(rdev),
	}, nil
}
