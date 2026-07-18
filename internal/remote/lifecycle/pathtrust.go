package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ensureTrustedDirectory creates missing path components one at a time and
// validates every existing component with Lstat. System ancestors such as /
// and /home need not be owned by the caller, but no symlink is accepted and a
// writable non-user ancestor is accepted only when it is a sticky directory
// (for example /tmp). Every caller-owned boundary must reject group/world
// writes; the final managed directories may additionally require an exact mode.
func ensureTrustedDirectory(path string, uid int, requireOwner bool, exactMode *os.FileMode) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("trusted directory path %q is not absolute", path)
	}
	components := absolutePathComponents(path)
	for index, component := range components {
		info, err := os.Lstat(component)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(component, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create trusted directory %q: %w", component, err)
			}
			info, err = os.Lstat(component)
		}
		if err != nil {
			return fmt.Errorf("inspect trusted directory %q: %w", component, err)
		}
		final := index == len(components)-1
		if err := validateDirectoryComponent(component, info, uid, final && requireOwner, final, exactMode); err != nil {
			return err
		}
	}
	return nil
}

func validateTrustedDirectory(path string, uid int, requireOwner bool, exactMode *os.FileMode) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("trusted directory path %q is not absolute", path)
	}
	components := absolutePathComponents(path)
	for index, component := range components {
		info, err := os.Lstat(component)
		if err != nil {
			return fmt.Errorf("inspect trusted directory %q: %w", component, err)
		}
		final := index == len(components)-1
		if err := validateDirectoryComponent(component, info, uid, final && requireOwner, final, exactMode); err != nil {
			return err
		}
	}
	return nil
}

func validateDirectoryComponent(path string, info os.FileInfo, uid int, requireOwner, final bool, exactMode *os.FileMode) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("trusted path component %q is not a non-symlink directory: %w", path, ErrUnsafeArtifact)
	}
	owner, ownerKnown := ownerUID(info)
	rootOwner, rootOwnerKnown := filesystemRootOwnerUID()
	if ownerKnown && owner != uid && owner != 0 && (!rootOwnerKnown || owner != rootOwner) {
		return fmt.Errorf("path component %q is owned by untrusted uid %d: %w", path, owner, ErrUnsafeArtifact)
	}
	if requireOwner && (!ownerKnown || owner != uid) {
		return fmt.Errorf("trusted directory %q is not owned by uid %d: %w", path, uid, ErrUnsafeArtifact)
	}
	permissions := info.Mode().Perm()
	if ownerKnown && owner == uid {
		if permissions&0o022 != 0 {
			return fmt.Errorf("caller-owned path component %q is group/world writable (%04o): %w", path, permissions, ErrUnsafeArtifact)
		}
	} else if permissions&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
		return fmt.Errorf("system path component %q is writable without the sticky bit (%04o): %w", path, permissions, ErrUnsafeArtifact)
	}
	if final && exactMode != nil && permissions != exactMode.Perm() {
		return fmt.Errorf("trusted directory %q mode is %04o, expected %04o: %w", path, permissions, exactMode.Perm(), ErrUnsafeArtifact)
	}
	return nil
}

func filesystemRootOwnerUID() (int, bool) {
	info, err := os.Lstat(string(filepath.Separator))
	if err != nil {
		return 0, false
	}
	return ownerUID(info)
}

func absolutePathComponents(path string) []string {
	volume := filepath.VolumeName(path)
	remainder := strings.TrimPrefix(path, volume)
	remainder = strings.TrimLeft(remainder, string(filepath.Separator))
	root := volume + string(filepath.Separator)
	components := []string{root}
	current := root
	for _, name := range strings.Split(remainder, string(filepath.Separator)) {
		if name == "" {
			continue
		}
		current = filepath.Join(current, name)
		components = append(components, current)
	}
	return components
}

func exactMode(mode os.FileMode) *os.FileMode { return &mode }
