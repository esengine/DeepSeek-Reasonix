//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func (m *SystemdManager) removeManagedArtifacts() (bool, error) {
	return m.removeManagedArtifactsWithHook(nil)
}

// removeManagedArtifactsWithHook keeps a deterministic adversarial test seam:
// the hook runs after both trusted directories are open but before any child is
// unlinked. All production calls pass nil.
func (m *SystemdManager) removeManagedArtifactsWithHook(beforeUnlink func()) (bool, error) {
	if err := validateTrustedDirectory(m.profile.ReasonixHome, m.uid, true, nil); err != nil {
		return false, fmt.Errorf("Reasonix Home removal boundary: %w", err)
	}
	if err := validateTrustedDirectory(m.managedRoot, m.uid, true, exactMode(0o700)); err != nil {
		return false, fmt.Errorf("managed root removal boundary: %w", err)
	}
	binInfo, err := os.Lstat(m.managedDir)
	binExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if binExists {
		if err := validateDirectoryComponent(m.managedDir, binInfo, m.uid, true, true, exactMode(0o700)); err != nil {
			return false, err
		}
	}

	homeFD, err := unix.Open(m.profile.ReasonixHome, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, fmt.Errorf("open Reasonix Home for removal: %w", err)
	}
	defer unix.Close(homeFD)
	rootFD, err := unix.Openat(homeFD, "remote", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, fmt.Errorf("open managed root for removal: %w", err)
	}
	defer unix.Close(rootFD)
	var rootStat, binStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil {
		return false, err
	}
	if err := validateDirectoryStat("managed root", rootStat, m.uid, 0o700); err != nil {
		return false, err
	}
	if !binExists {
		if beforeUnlink != nil {
			beforeUnlink()
		}
		if !pathMatchesStat(m.managedRoot, rootStat) {
			return false, fmt.Errorf("managed root path was rebound before removal: %w", ErrUnsafeArtifact)
		}
		err := unix.Unlinkat(homeFD, "remote", unix.AT_REMOVEDIR)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, unix.ENOTEMPTY) || errors.Is(err, unix.EEXIST) {
			return false, nil
		}
		return false, fmt.Errorf("remove empty managed root directory: %w", err)
	}

	binFD, err := unix.Openat(rootFD, "bin", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, fmt.Errorf("open managed bin for removal: %w", err)
	}
	defer unix.Close(binFD)
	if err := unix.Fstat(binFD, &binStat); err != nil {
		return false, err
	}
	if err := validateDirectoryStat("managed bin", binStat, m.uid, 0o700); err != nil {
		return false, err
	}

	if beforeUnlink != nil {
		beforeUnlink()
	}
	if !pathMatchesStat(m.managedRoot, rootStat) || !pathMatchesStat(m.managedDir, binStat) {
		return false, fmt.Errorf("managed directory path was rebound before removal: %w", ErrUnsafeArtifact)
	}

	changed := false
	for _, artifact := range []struct {
		name       string
		executable bool
	}{
		{name: "reasonix", executable: true},
		{name: ManifestName},
	} {
		removed, err := removeTrustedRegularAt(binFD, artifact.name, m.uid, artifact.executable)
		if err != nil {
			return changed, err
		}
		changed = changed || removed
	}

	// unlinkat never follows a replacement symlink with AT_REMOVEDIR. Unknown
	// entries intentionally leave bin/root in place.
	if pathMatchesStat(m.managedDir, binStat) {
		err := unix.Unlinkat(rootFD, "bin", unix.AT_REMOVEDIR)
		if err == nil {
			changed = true
		} else if !errors.Is(err, unix.ENOTEMPTY) && !errors.Is(err, unix.EEXIST) {
			return changed, fmt.Errorf("remove empty managed bin directory: %w", err)
		}
	}
	if pathMatchesStat(m.managedRoot, rootStat) {
		err := unix.Unlinkat(homeFD, "remote", unix.AT_REMOVEDIR)
		if err == nil {
			changed = true
		} else if !errors.Is(err, unix.ENOTEMPTY) && !errors.Is(err, unix.EEXIST) {
			return changed, fmt.Errorf("remove empty managed root directory: %w", err)
		}
	}
	return changed, nil
}

func validateDirectoryStat(label string, stat unix.Stat_t, uid int, mode uint32) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || int(stat.Uid) != uid || stat.Mode&0o777 != mode {
		return fmt.Errorf("%s fd is not the expected owned %04o directory: %w", label, mode, ErrUnsafeArtifact)
	}
	return nil
}

func pathMatchesStat(path string, want unix.Stat_t) bool {
	var got unix.Stat_t
	if err := unix.Lstat(path, &got); err != nil || got.Mode&unix.S_IFMT == unix.S_IFLNK {
		return false
	}
	return got.Dev == want.Dev && got.Ino == want.Ino
}

func removeTrustedRegularAt(directoryFD int, name string, uid int, executable bool) (bool, error) {
	var stat unix.Stat_t
	err := unix.Fstatat(directoryFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	permissions := stat.Mode & 0o777
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || int(stat.Uid) != uid || permissions&0o022 != 0 || executable && permissions&0o100 == 0 {
		return false, fmt.Errorf("managed artifact %q is not a trusted owned regular file: %w", name, ErrUnsafeArtifact)
	}
	if err := unix.Unlinkat(directoryFD, name, 0); err != nil {
		return false, fmt.Errorf("unlink managed artifact %q: %w", name, err)
	}
	return true, nil
}
