package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"reasonix/internal/remote"
	"reasonix/internal/remote/sftpfs"
)

type releaseRaceFS struct {
	*sftpfs.FS
	mkdir func(context.Context, string) error
	stat  func(context.Context, string) (sftpfs.Entry, error)
}

func (f releaseRaceFS) MkdirExclusive(ctx context.Context, path string) error {
	return f.mkdir(ctx, path)
}

func (f releaseRaceFS) Stat(ctx context.Context, path string) (sftpfs.Entry, error) {
	if f.stat != nil {
		return f.stat(ctx, path)
	}
	return f.FS.Stat(ctx, path)
}

func TestServeLockAcquiresAfterObservedOwnerRelease(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
	paths := pathsFor(root, root)
	owner, err := acquireServeLock(context.Background(), conn.fs, paths, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.release)
	calls := 0
	wrapped := releaseRaceFS{FS: conn.fs, mkdir: func(ctx context.Context, path string) error {
		calls++
		err := conn.fs.MkdirExclusive(ctx, path)
		if calls == 1 {
			if err == nil {
				t.Fatal("first owner was not exclusive")
			}
			owner.release() // deterministically release after mkdir failed, before Stat
		}
		return err
	}}
	next, err := acquireServeLock(context.Background(), wrapped, paths, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer next.release()
	if calls != 2 || next.owner == owner.owner {
		t.Fatalf("acquisition calls=%d, replacement owner unique=%v", calls, next.owner != owner.owner)
	}
}

func TestServeLockDoesNotRetryNonMissingObservations(t *testing.T) {
	skipOnWindows(t)
	for _, statErr := range []error{os.ErrPermission, errors.New("stat disconnected"), nil} {
		root := t.TempDir()
		conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
		calls := 0
		wrapped := releaseRaceFS{FS: conn.fs,
			mkdir: func(context.Context, string) error { calls++; return os.ErrExist },
			stat: func(context.Context, string) (sftpfs.Entry, error) {
				return sftpfs.Entry{IsDir: false}, statErr
			},
		}
		_, err := acquireServeLock(context.Background(), wrapped, pathsFor(root, root), time.Now)
		if !errors.Is(err, os.ErrExist) || calls != 1 {
			t.Fatalf("stat=%v error=%v calls=%d; expected immediate creation failure", statErr, err, calls)
		}
	}
}

func TestServeLockMissingObservationRetriesAreBounded(t *testing.T) {
	skipOnWindows(t)
	for _, tc := range []struct {
		name      string
		err       error
		cancel    bool
		wantCalls int
	}{
		{"generic-failure", &sftp.StatusError{Code: uint32(sftp.ErrSSHFxFailure)}, false, 2},
		{"exists", os.ErrExist, false, 2},
		{"permission", os.ErrPermission, false, 1},
		{"transport", errors.New("transport disconnected"), false, 1},
		{"cancelled", os.ErrExist, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			wrapped := releaseRaceFS{FS: conn.fs, mkdir: func(context.Context, string) error {
				calls++
				if tc.cancel {
					cancel()
				}
				return fmt.Errorf("mkdir: %w", tc.err)
			}}
			_, err := acquireServeLock(ctx, wrapped, pathsFor(root, root), time.Now)
			wantErr := tc.err
			if tc.cancel {
				wantErr = context.Canceled
			}
			if !errors.Is(err, wantErr) || calls != tc.wantCalls {
				t.Fatalf("error=%v calls=%d, want %v/%d", err, calls, wantErr, tc.wantCalls)
			}
		})
	}
}
