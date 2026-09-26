package fileutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
	tmp, err := os.CreateTemp(dir, ".gitignore-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := writeMarker(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	// Link publishes only a complete marker and, like O_EXCL, refuses to
	// replace or follow whatever already sits at the target: an empty marker
	// left by a failed write would read as present and never be repaired.
	err = os.Link(tmp.Name(), filepath.Join(dir, ".gitignore"))
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	return err
}
