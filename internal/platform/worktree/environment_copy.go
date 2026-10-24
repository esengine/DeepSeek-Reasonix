package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func readEnvironmentEntry(ctx context.Context, root *os.Root, path string) (environmentEntry, error) {
	entry := environmentEntry{Path: path}
	if err := ctx.Err(); err != nil {
		return entry, err
	}
	if err := safeEnvironmentPath(path); err != nil {
		return entry, err
	}
	if err := plainEnvironmentParents(root, path); err != nil {
		return entry, err
	}
	info, err := root.Lstat(path)
	if err != nil {
		return entry, err
	}
	entry.Mode = info.Mode()
	if info.IsDir() {
		return entry, nil
	}
	if !info.Mode().IsRegular() || info.Size() > environmentMaxBytes {
		return entry, fmt.Errorf("%w: unsupported input type or size: %s", ErrEnvironment, path)
	}
	file, err := root.Open(path)
	if err != nil {
		return entry, err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, &environmentReader{ctx: ctx, reader: io.LimitReader(file, environmentMaxBytes+1)})
	if err != nil {
		return entry, err
	}
	after, err := file.Stat()
	current, currentErr := root.Lstat(path)
	if err != nil || currentErr != nil || n != info.Size() || !sameEnvironmentFile(info, after) || !sameEnvironmentFile(info, current) {
		return entry, errors.Join(ErrWorkspaceMoved, err, currentErr)
	}
	entry.Size, entry.Digest = n, hex.EncodeToString(hash.Sum(nil))
	return entry, nil
}

func sameEnvironmentFile(before, after os.FileInfo) bool {
	return os.SameFile(before, after) && before.Mode() == after.Mode() && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

func plainEnvironmentParents(root *os.Root, path string) error {
	for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
		info, err := root.Lstat(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: input parent must be a plain directory", ErrEnvironment)
		}
	}
	return nil
}

func copyEnvironmentEntry(ctx context.Context, source, target *os.Root, entry environmentEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := plainEnvironmentParents(target, entry.Path); err != nil {
		return err
	}
	info, err := target.Lstat(entry.Path)
	if err == nil {
		if entry.Mode.IsDir() {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return ErrEnvironment
			}
			return target.Chmod(entry.Path, entry.Mode.Perm()|0700)
		}
		actual, err := readEnvironmentEntry(ctx, target, entry.Path)
		if err != nil || actual.Digest != entry.Digest {
			return errors.Join(ErrWorkspaceMoved, err)
		}
		return target.Chmod(entry.Path, entry.Mode.Perm())
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if entry.Mode.IsDir() {
		return target.Mkdir(entry.Path, entry.Mode.Perm()|0700)
	}
	return copyEnvironmentFile(ctx, source, target, entry)
}

func copyEnvironmentFile(ctx context.Context, source, target *os.Root, entry environmentEntry) error {
	input, err := source.Open(entry.Path)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := target.OpenFile(entry.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, entry.Mode.Perm())
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(output, hash), &environmentReader{ctx: ctx, reader: io.LimitReader(input, entry.Size+1)})
	if copyErr == nil {
		copyErr = output.Chmod(entry.Mode.Perm())
	}
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if n != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.Digest {
		copyErr = errors.Join(copyErr, ErrWorkspaceMoved)
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	actual, err := readEnvironmentEntry(ctx, target, entry.Path)
	if err != nil || actual != entry {
		return errors.Join(ErrWorkspaceMoved, err)
	}
	return nil
}

type environmentReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *environmentReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
