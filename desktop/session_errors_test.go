package main

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// Platform errnos for the blocked-file classes. Windows values follow the
// isRenameCrossDeviceOrBusy convention of naming the raw code inline.
func blockedFileErrnos(t *testing.T) (inUse, accessDenied, diskFull syscall.Errno) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return 32, 5, 112 // ERROR_SHARING_VIOLATION, ERROR_ACCESS_DENIED, ERROR_DISK_FULL
	}
	return syscall.EBUSY, syscall.EACCES, syscall.ENOSPC
}

func TestFriendlySessionFileErrorMapsBlockedFileErrors(t *testing.T) {
	inUse, accessDenied, diskFull := blockedFileErrnos(t)
	secret := "/very/secret/session.jsonl"

	cases := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "rename sharing violation",
			err:  &os.LinkError{Op: "rename", Old: secret, New: secret + ".trash", Err: inUse},
			want: errSessionFileLocked,
		},
		{
			name: "remove access denied",
			err:  &os.PathError{Op: "remove", Path: secret, Err: accessDenied},
			want: errSessionFileAccessDenied,
		},
		{
			name: "write disk full",
			err:  &os.PathError{Op: "write", Path: secret, Err: diskFull},
			want: errSessionDiskFull,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := friendlySessionFileError(tc.err)
			if got != tc.want {
				t.Fatalf("friendlySessionFileError() = %v, want %v", got, tc.want)
			}
			if strings.Contains(got.Error(), secret) {
				t.Fatalf("sanitized error leaks the path: %v", got)
			}
		})
	}
}

func TestFriendlySessionFileErrorPassesThroughOtherErrors(t *testing.T) {
	if err := friendlySessionFileError(nil); err != nil {
		t.Fatalf("nil should stay nil, got %v", err)
	}
	plain := errors.New("plain failure")
	if got := friendlySessionFileError(plain); got != plain {
		t.Fatalf("plain error rewritten to %v", got)
	}
	if got := friendlySessionFileError(errSessionBusyElsewhere); got != errSessionBusyElsewhere {
		t.Fatalf("sanitized busy error rewritten to %v", got)
	}
	notExist := &os.PathError{Op: "lstat", Path: "gone.jsonl", Err: syscall.ENOENT}
	if got := friendlySessionFileError(notExist); got != error(notExist) {
		t.Fatalf("not-exist error rewritten to %v", got)
	}
}
