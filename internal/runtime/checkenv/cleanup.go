package checkenv

import (
	"io/fs"
	"os"
)

func removeFrozenTree(directory string) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return
	}
	_ = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.IsDir() {
			return root.Chmod(path, 0700)
		}
		return walkErr
	})
	root.Close()
	_ = os.RemoveAll(directory)
}
