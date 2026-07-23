package bootstrap

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/remote/sftpfs"
)

const (
	serveLockPoll       = 100 * time.Millisecond
	serveLockStaleAfter = 60 * time.Second
)

type serveLock struct {
	fs    *sftpfs.FS
	paths StatePaths
	owner string
}

// acquireServeLock serializes the short launch/publish critical section across
// CLI processes, desktop windows, and reconnect generations. The expensive
// locate/install phase stays outside the lock. A crashed owner's directory is
// reclaimed only after a minute; the guarded health check itself is bounded to
// 20 seconds, so a live owner cannot legitimately age past that threshold.
func acquireServeLock(ctx context.Context, fs *sftpfs.FS, paths StatePaths, clock func() time.Time) (*serveLock, error) {
	if err := fs.MkdirAll(ctx, paths.Dir); err != nil {
		return nil, err
	}
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	owner := strconv.FormatInt(clock().Unix(), 10) + ":" + token
	for {
		mkdirErr := fs.MkdirExclusive(ctx, paths.LockDir)
		if mkdirErr == nil {
			if err := fs.WriteFileAtomic(ctx, paths.LockOwner, []byte(owner+"\n"), 0o600); err != nil {
				_ = fs.Remove(context.Background(), paths.LockDir, true)
				return nil, fmt.Errorf("bootstrap: write serve lock owner: %w", err)
			}
			return &serveLock{fs: fs, paths: paths, owner: owner}, nil
		}

		lockInfo, statErr := fs.Stat(ctx, paths.LockDir)
		if statErr != nil || !lockInfo.IsDir {
			return nil, fmt.Errorf("bootstrap: create serve lock: %w", mkdirErr)
		}
		data, _, _, readErr := fs.ReadFile(ctx, paths.LockOwner, 512)
		if readErr == nil {
			observed := strings.TrimSpace(string(data))
			parts := strings.SplitN(observed, ":", 2)
			created, parseErr := strconv.ParseInt(parts[0], 10, 64)
			if parseErr == nil && len(parts) == 2 && clock().Sub(time.Unix(created, 0)) > serveLockStaleAfter {
				// Compare the owner again immediately before removal. A new owner never
				// inherits the old random token, so we cannot delete a replacement lock.
				current, _, _, currentErr := fs.ReadFile(ctx, paths.LockOwner, 512)
				if currentErr == nil && strings.TrimSpace(string(current)) == observed {
					_ = fs.Remove(ctx, paths.LockDir, true)
					continue
				}
			}
		} else if clock().Sub(time.Unix(lockInfo.ModTime, 0)) > serveLockStaleAfter {
			// The creator may have crashed between mkdir and writing owner. The
			// critical section cannot legitimately leave an owner-less directory
			// this old, so reclaim it.
			if _, _, _, currentErr := fs.ReadFile(ctx, paths.LockOwner, 512); currentErr != nil {
				_ = fs.Remove(ctx, paths.LockDir, true)
				continue
			}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("bootstrap: wait for serve lock: %w", ctx.Err())
		case <-time.After(serveLockPoll):
		}
	}
}

func (l *serveLock) release() {
	if l == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	data, _, _, err := l.fs.ReadFile(ctx, l.paths.LockOwner, 512)
	if err == nil && strings.TrimSpace(string(data)) == l.owner {
		_ = l.fs.Remove(ctx, l.paths.LockDir, true)
	}
}
