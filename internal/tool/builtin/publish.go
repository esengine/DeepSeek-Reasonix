package builtin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
)

// ErrFileChanged is returned when a structured write tool re-reads the target
// and the existence, SHA-256, or mode no longer match the first read.
var ErrFileChanged = errors.New("file changed after it was read")

type fileIdentity struct {
	existed bool
	overlay bool
	sum     [sha256.Size]byte
	mode    os.FileMode
}

func diskIdentity(path string) (fileIdentity, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileIdentity{}, nil
		}
		return fileIdentity{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fileIdentity{}, err
	}
	return fileIdentity{existed: true, sum: sha256.Sum256(b), mode: fi.Mode().Perm()}, nil
}

func overlayIdentity(content string) fileIdentity {
	return fileIdentity{existed: true, overlay: true, sum: sha256.Sum256([]byte(content))}
}

func (id fileIdentity) equal(other fileIdentity) bool {
	return id.existed == other.existed && id.overlay == other.overlay &&
		id.sum == other.sum && id.mode == other.mode
}

func (s editSource) assertUnchanged(ctx context.Context, overlay FileOverlay, path string) error {
	now, err := s.currentIdentity(ctx, overlay, path)
	if err != nil {
		return err
	}
	if !s.id.equal(now) {
		return fmt.Errorf("%w: %s", ErrFileChanged, path)
	}
	return nil
}

func (s editSource) currentIdentity(ctx context.Context, overlay FileOverlay, path string) (fileIdentity, error) {
	if s.overlay && overlay != nil {
		if buffered, ok := overlay.ReadTextFile(ctx, path); ok {
			return overlayIdentity(buffered), nil
		}
	}
	return diskIdentity(path)
}
