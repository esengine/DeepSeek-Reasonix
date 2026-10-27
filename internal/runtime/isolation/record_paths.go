package isolation

import (
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
)

type recordDirectory struct {
	root      *os.Root
	canonical string
}

func openRecordDirectory(path string) (recordDirectory, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return recordDirectory{}, err
	}
	dir, err := wrapRecordDirectory(root)
	if err != nil {
		root.Close()
	}
	return dir, err
}

func wrapRecordDirectory(root *os.Root) (recordDirectory, error) {
	canonical, err := fileutil.ResolveExistingPath(root.Name())
	return recordDirectory{root: root, canonical: canonical}, err
}

// local resolves spelling aliases for identity, then confines every subsequent
// stat/read to the opened root. Resolution does not authorize a file-content read.
func (d recordDirectory) local(claim string) (string, bool) {
	if claim == "" || !filepath.IsAbs(claim) {
		return "", false
	}
	path, err := fileutil.ResolveExistingPath(claim)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(d.canonical, path)
	return rel, err == nil && filepath.IsLocal(rel)
}

func (d recordDirectory) same(claim, expected string) bool {
	rel, ok := d.local(claim)
	if !ok {
		return false
	}
	left, err := d.root.Stat(rel)
	if err != nil || !left.IsDir() {
		return false
	}
	right, err := d.root.Stat(expected)
	return err == nil && right.IsDir() && os.SameFile(left, right)
}

func recordDirectoryEntry(root *os.Root, name string) bool {
	info, err := root.Lstat(name)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func recordRegularFile(root *os.Root, name string) bool {
	info, err := root.Lstat(name)
	return err == nil && info.Mode().IsRegular()
}
