package testdata

import (
	"os"
	"path/filepath"
	"runtime"
)

func InvalidUTF8() ([]byte, error) {
	return load("UTF-8-test.txt")
}

func Sample() ([]byte, error) {
	return load("sample.txt")
}

func load(filename string) ([]byte, error) {
	// Get the directory of this source file
	_, currentFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(currentFile)
	path := filepath.Join(dir, filename)

	return os.ReadFile(path)
}
