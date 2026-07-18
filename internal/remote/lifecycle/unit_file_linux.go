//go:build linux

package lifecycle

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (m *SystemdManager) readTrustedUnitFile(limit int64) ([]byte, FileStatus, fileIdentity, error) {
	status := FileStatus{Path: m.unitPath}
	parent := filepath.Dir(m.unitPath)
	if err := validateTrustedDirectory(parent, m.uid, true, nil); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, status, fileIdentity{}, os.ErrNotExist
		}
		return nil, status, fileIdentity{}, err
	}
	parentFD, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, status, fileIdentity{}, err
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat(parentFD, filepath.Base(m.unitPath), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, status, fileIdentity{}, os.ErrNotExist
	}
	if err != nil {
		return nil, status, fileIdentity{}, err
	}
	file := os.NewFile(uintptr(fd), m.unitPath)
	if file == nil {
		unix.Close(fd)
		return nil, status, fileIdentity{}, ErrUnsafeArtifact
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, status, fileIdentity{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || int(stat.Uid) != m.uid || stat.Mode&0o022 != 0 {
		return nil, status, fileIdentity{}, fmt.Errorf("unit is not a trusted owned regular file: %w", ErrUnsafeArtifact)
	}
	info, err := file.Stat()
	if err != nil {
		return nil, status, fileIdentity{}, err
	}
	status.Exists = true
	status.Kind = "regular"
	status.Mode = uint32(info.Mode())
	status.ModeText = info.Mode().String()
	status.UID = int64(stat.Uid)
	status.OwnerKnown = true
	status.OwnerMatches = true
	status.Secure = true
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, status, fileIdentity{}, err
	}
	if int64(len(contents)) > limit {
		return nil, status, fileIdentity{}, errors.New("unit file exceeds size limit")
	}
	return contents, status, fileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}
