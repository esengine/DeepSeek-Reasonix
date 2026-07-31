package sftp

import (
	"errors"
	"fmt"
	"io"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrFxCode(t *testing.T) {
	table := []struct {
		err error
		fx  fxerr
	}{
		{err: errors.New("random error"), fx: ErrSSHFxFailure},
		{err: EBADF, fx: ErrSSHFxFailure},
		{err: syscall.ENOENT, fx: ErrSSHFxNoSuchFile},
		{err: syscall.EPERM, fx: ErrSSHFxPermissionDenied},
		{err: io.EOF, fx: ErrSSHFxEOF},
		{err: fmt.Errorf("wrapped permission denied error: %w", ErrSSHFxPermissionDenied), fx: ErrSSHFxPermissionDenied},
		{err: fmt.Errorf("wrapped op unsupported error: %w", ErrSSHFxOpUnsupported), fx: ErrSSHFxOpUnsupported},
	}
	for _, tt := range table {
		statusErr := statusFromError(1, tt.err).StatusError
		assert.Equal(t, statusErr.FxCode(), tt.fx)
	}
}

func TestSupportedExtensions(t *testing.T) {
	for _, supportedExtension := range supportedSFTPExtensions {
		_, err := getSupportedExtensionByName(supportedExtension.Name)
		assert.NoError(t, err)
	}
	_, err := getSupportedExtensionByName("invalid@example.com")
	assert.Error(t, err)
}

func TestExtensions(t *testing.T) {
	var supportedExtensions []string
	for _, supportedExtension := range supportedSFTPExtensions {
		supportedExtensions = append(supportedExtensions, supportedExtension.Name)
	}

	testSFTPExtensions := []string{"hardlink@openssh.com"}
	expectedSFTPExtensions := []sshExtensionPair{
		{"hardlink@openssh.com", "1"},
	}
	err := SetSFTPExtensions(testSFTPExtensions...)
	assert.NoError(t, err)
	assert.Equal(t, expectedSFTPExtensions, sftpExtensions)

	invalidSFTPExtensions := []string{"invalid@example.com"}
	err = SetSFTPExtensions(invalidSFTPExtensions...)
	assert.Error(t, err)
	assert.Equal(t, expectedSFTPExtensions, sftpExtensions)

	emptySFTPExtensions := []string{}
	expectedSFTPExtensions = []sshExtensionPair{}
	err = SetSFTPExtensions(emptySFTPExtensions...)
	assert.NoError(t, err)
	assert.Equal(t, expectedSFTPExtensions, sftpExtensions)

	// if we only have an invalid extension nothing will be modified.
	invalidSFTPExtensions = []string{
		"hardlink@openssh.com",
		"invalid@example.com",
	}
	err = SetSFTPExtensions(invalidSFTPExtensions...)
	assert.Error(t, err)
	assert.Equal(t, expectedSFTPExtensions, sftpExtensions)

	err = SetSFTPExtensions(supportedExtensions...)
	assert.NoError(t, err)
	assert.Equal(t, supportedSFTPExtensions, sftpExtensions)
}
