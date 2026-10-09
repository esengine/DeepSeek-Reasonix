package sessionv4

import (
	"io/fs"
	"os"
)

// rootOpened, when set, is told about every os.Root dirFS opens.
var rootOpened func(*os.Root)

// dirFS is a directory read through an os.Root that lives only as long as the
// file being read, so a Session found by path keeps no handle on the store
// between reads. A symlink inside the directory that leaves it is refused.
type dirFS string

func (d dirFS) Open(name string) (fs.File, error) {
	root, err := os.OpenRoot(string(d))
	if err != nil {
		return nil, err
	}
	f, err := root.FS().Open(name)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if rootOpened != nil {
		rootOpened(root)
	}
	return &rootedFile{File: f, root: root}, nil
}

type rootedFile struct {
	fs.File
	root *os.Root
}

func (f *rootedFile) Close() error {
	err := f.File.Close()
	if cerr := f.root.Close(); err == nil {
		err = cerr
	}
	return err
}

func (f *rootedFile) ReadDir(n int) ([]fs.DirEntry, error) {
	return f.File.(fs.ReadDirFile).ReadDir(n)
}
