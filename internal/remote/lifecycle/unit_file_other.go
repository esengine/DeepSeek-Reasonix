//go:build !linux

package lifecycle

import (
	"errors"
	"io"
	"os"
)

func (m *SystemdManager) readTrustedUnitFile(limit int64) ([]byte, FileStatus, fileIdentity, error) {
	status := inspectFile(m.unitPath, m.uid)
	status.Secure = secureRegularData(status)
	if !status.Exists {
		return nil, status, fileIdentity{}, os.ErrNotExist
	}
	if !status.Secure {
		return nil, status, fileIdentity{}, ErrUnsafeArtifact
	}
	file, err := os.Open(m.unitPath)
	if err != nil {
		return nil, status, fileIdentity{}, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, status, fileIdentity{}, err
	}
	if int64(len(contents)) > limit {
		return nil, status, fileIdentity{}, errors.New("unit file exceeds size limit")
	}
	return contents, status, fileIdentity{}, nil
}
