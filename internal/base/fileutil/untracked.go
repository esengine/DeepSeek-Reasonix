package fileutil

import (
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
)

var writeMarker = func(f *os.File) error {
	_, err := f.WriteString("*\n")
	return err
}

// MarkUntracked writes a .gitignore of "*" into dir, so git leaves the whole
// directory — the marker included — out of `git status`; an existing marker is
// kept, whoever wrote it. It is a courtesy to the user's git, never a condition
// of the write it accompanies, so callers ignore its error: a filesystem with
// no hard links (FAT, some network mounts) must still store the state.
func MarkUntracked(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return MarkUntrackedInRoot(root)
}

// MarkUntrackedInRoot keeps marker creation and publication on an already-open
// directory, even if its pathname is replaced during an attachment write.
func MarkUntrackedInRoot(root *os.Root) error {
	name := ".gitignore-" + rand.Text()
	tmp, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(name) }()
	if err := writeMarker(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	err = root.Link(name, ".gitignore")
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	return err
}
