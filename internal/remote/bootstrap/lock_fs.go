package bootstrap

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/pkg/sftp"

	"reasonix/internal/remote/sftpfs"
)

type serveLockFS interface {
	MkdirAll(context.Context, string) error
	MkdirExclusive(context.Context, string) error
	Stat(context.Context, string) (sftpfs.Entry, error)
	ReadFile(context.Context, string, int64) ([]byte, bool, sftpfs.Kind, error)
	WriteFileAtomic(context.Context, string, []byte, fs.FileMode) error
	Remove(context.Context, string, bool) error
}

func lockCreationMayContend(err error) bool {
	if errors.Is(err, os.ErrExist) {
		return true
	}
	var status *sftp.StatusError
	return errors.As(err, &status) && status.FxCode() == sftp.ErrSSHFxFailure
}
