//go:build linux

package lifecycle

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type trustedUnitRemoval struct {
	parentPath   string
	name         string
	parentFD     int
	parentStat   unix.Stat_t
	unitStat     unix.Stat_t
	beforeUnlink func()
}

func (m *SystemdManager) prepareUnitRemoval(expected fileIdentity) (*trustedUnitRemoval, error) {
	parent := filepath.Dir(m.unitPath)
	if err := validateTrustedDirectory(parent, m.uid, true, nil); err != nil {
		return nil, fmt.Errorf("unit removal parent: %w", err)
	}
	parentFD, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*trustedUnitRemoval, error) {
		unix.Close(parentFD)
		return nil, err
	}
	var parentStat, unitStat unix.Stat_t
	if err := unix.Fstat(parentFD, &parentStat); err != nil {
		return fail(err)
	}
	name := filepath.Base(m.unitPath)
	if err := unix.Fstatat(parentFD, name, &unitStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fail(err)
	}
	permissions := unitStat.Mode & 0o777
	if unitStat.Mode&unix.S_IFMT != unix.S_IFREG || int(unitStat.Uid) != m.uid || permissions&0o022 != 0 {
		return fail(fmt.Errorf("unit removal target is not a trusted owned regular file: %w", ErrUnsafeArtifact))
	}
	if !expected.matches(uint64(unitStat.Dev), unitStat.Ino) {
		return fail(fmt.Errorf("unit inode changed after exact content validation: %w", ErrUnsafeArtifact))
	}
	return &trustedUnitRemoval{
		parentPath: parent, name: name, parentFD: parentFD,
		parentStat: parentStat, unitStat: unitStat,
	}, nil
}

func (removal *trustedUnitRemoval) Remove() error {
	if removal == nil || removal.parentFD < 0 {
		return fmt.Errorf("unit removal handle is unavailable: %w", ErrUnsafeArtifact)
	}
	if removal.beforeUnlink != nil {
		removal.beforeUnlink()
	}
	if !pathMatchesStat(removal.parentPath, removal.parentStat) {
		return fmt.Errorf("unit parent was rebound before unlink: %w", ErrUnsafeArtifact)
	}
	var current unix.Stat_t
	if err := unix.Fstatat(removal.parentFD, removal.name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Dev != removal.unitStat.Dev || current.Ino != removal.unitStat.Ino || current.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("unit path was rebound before unlink: %w", ErrUnsafeArtifact)
	}
	if err := unix.Unlinkat(removal.parentFD, removal.name, 0); err != nil {
		return err
	}
	return unix.Fsync(removal.parentFD)
}

func (removal *trustedUnitRemoval) Close() error {
	if removal == nil || removal.parentFD < 0 {
		return nil
	}
	fd := removal.parentFD
	removal.parentFD = -1
	return unix.Close(fd)
}
