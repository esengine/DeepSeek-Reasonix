package main

import fileencoding "reasonix/internal/fileutil/encoding"

func readFileUTF8(path string) ([]byte, error) {
	return fileencoding.ReadFileUTF8(path)
}
