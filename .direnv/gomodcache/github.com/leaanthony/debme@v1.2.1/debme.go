package debme

import (
	"embed"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type embedFS = embed.FS

// Debme is an embed.FS compatible wrapper, providing Sub() functionality
type Debme struct {
	basedir string
	embedFS
}

// FS creates an embed.FS compatible struct, anchored to the given basedir.
func FS(fs embed.FS, basedir string) (Debme, error) {
	result := Debme{embedFS: fs, basedir: basedir}
	_, err := result.ReadDir(".")
	if err != nil {
		return Debme{}, err
	}
	return result, nil
}

func (d Debme) calculatePath(path string) string {
	base := filepath.Join(d.basedir, path)
	return filepath.ToSlash(base)
}

// Open opens the named file for reading and returns it as an fs.File.
func (d Debme) Open(name string) (fs.File, error) {
	path := d.calculatePath(name)
	return d.embedFS.Open(path)
}

// ReadDir reads and returns the entire named directory.
func (d Debme) ReadDir(name string) ([]fs.DirEntry, error) {
	path := d.calculatePath(name)
	return d.embedFS.ReadDir(path)
}

// ReadFile reads and returns the content of the named file.
func (d Debme) ReadFile(name string) ([]byte, error) {
	path := d.calculatePath(name)
	return d.embedFS.ReadFile(path)
}

// FS returns a new Debme anchored at the given subdirectory.
func (d Debme) FS(subDir string) (Debme, error) {
	path := d.calculatePath(subDir)
	return FS(d.embedFS, path)
}

func (d Debme) CopyFile(sourcePath string, target string, perm os.FileMode) error {
	sourceFile, err := d.Open(sourcePath)
	if err != nil {
		return err
	}
	targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, perm)
	if err != nil {
		return err
	}
	_, err = io.Copy(targetFile, sourceFile)
	return err
}
