package checkenv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"time"
)

type treePolicy struct {
	source bool
	denied []string
}

type treeEntry struct {
	Path     string
	Mode     fs.FileMode
	Size     int64
	Modified int64
	Link     string
	Digest   string
}

// unreproducedModeBits are not copied into a frozen tree; an unprivileged
// payload cannot use them, so they are not part of a verification input.
const unreproducedModeBits = fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

func scanTree(ctx context.Context, directory string, policy treePolicy) ([]treeEntry, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var entries []treeEntry
	var total int64
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if policy.source && path == ".git" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if deniedPath(path, policy.denied) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if len(entries) >= 200_000 {
			return errors.New("verification input exceeds entry limit")
		}
		info, err := root.Lstat(path)
		if err != nil {
			return err
		}
		entry := treeEntry{Path: path, Mode: info.Mode() &^ unreproducedModeBits, Size: info.Size(), Modified: info.ModTime().UnixNano()}
		switch {
		case info.Mode().IsRegular():
			total += info.Size()
			if total > 2<<30 {
				return errors.New("verification input exceeds byte limit")
			}
			file, err := root.Open(path)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, readErr := io.Copy(h, file)
			err = errors.Join(readErr, file.Close())
			if err != nil {
				return err
			}
			entry.Digest = hex.EncodeToString(h.Sum(nil))
		case info.IsDir():
			entry.Size = 0
		case info.Mode()&fs.ModeSymlink != 0:
			entry.Link, err = root.Readlink(path)
			if err != nil {
				return err
			}
			entry.Size, entry.Modified = 0, 0
		default:
			return errors.New("verification input contains a special file")
		}
		entries = append(entries, entry)
		return nil
	})
	return entries, err
}

func freezeTree(ctx context.Context, source, target string, policy treePolicy) (string, error) {
	before, err := scanTree(ctx, source, policy)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return "", err
	}
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return "", err
	}
	defer sourceRoot.Close()
	targetRoot, err := os.OpenRoot(target)
	if err != nil {
		return "", err
	}
	defer targetRoot.Close()
	for _, entry := range before {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		switch {
		case entry.Mode.IsDir():
			if entry.Path != "." {
				err = targetRoot.Mkdir(entry.Path, 0700)
			}
		case entry.Mode&fs.ModeSymlink != 0:
			err = targetRoot.Symlink(entry.Link, entry.Path)
		default:
			err = copyEntry(sourceRoot, targetRoot, entry)
		}
		if err != nil {
			return "", err
		}
	}
	for _, entry := range slices.Backward(before) {
		if entry.Mode&fs.ModeSymlink != 0 {
			continue
		}
		if err := targetRoot.Chmod(entry.Path, entry.Mode.Perm()); err != nil {
			return "", err
		}
		timestamp := time.Unix(0, entry.Modified)
		if err := targetRoot.Chtimes(entry.Path, timestamp, timestamp); err != nil {
			return "", err
		}
	}
	after, err := scanTree(ctx, source, policy)
	if err != nil || !slices.Equal(before, after) {
		return "", errors.Join(err, errors.New("verification input changed during freezing"))
	}
	copied, err := scanTree(ctx, target, treePolicy{})
	if err != nil || !slices.Equal(before, copied) {
		return "", errors.Join(err, errors.New("verification copy differs from its input"))
	}
	return digest(copied), nil
}

func copyEntry(source, target *os.Root, entry treeEntry) error {
	input, err := source.Open(entry.Path)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := target.OpenFile(entry.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}
