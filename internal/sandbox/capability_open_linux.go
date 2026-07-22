//go:build linux

package sandbox

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openCapabilityDescriptor binds Bubblewrap to the verified object rather than
// asking it to resolve a host pathname later. RESOLVE_NO_SYMLINKS closes the
// replacement window across every path component, including magic links.
func openCapabilityDescriptor(path CapabilityPath) (*os.File, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, path.Canonical, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path.Canonical)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("adopt descriptor for %q", path.Canonical)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	kind, err := capabilityPathKind(info)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("path %q: %w", path.Canonical, err)
	}
	if kind != path.Kind {
		_ = file.Close()
		return nil, fmt.Errorf("path %q changed kind from %s to %s", path.Canonical, path.Kind, kind)
	}
	return file, nil
}
