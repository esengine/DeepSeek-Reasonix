package main

import (
	"fmt"
	"io"
	"os"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func readFileUTF8(path string) ([]byte, error) {
	return fileencoding.ReadFileUTF8(path)
}

func readFileUTF8Limit(path string, limit int64) ([]byte, bool, error) {
	if limit < 0 {
		return nil, false, fmt.Errorf("invalid read limit %d", limit)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(raw)) > limit {
		return nil, true, nil
	}
	decoded := fileencoding.DecodeToUTF8(raw)
	if int64(len(decoded)) > limit {
		return nil, true, nil
	}
	return decoded, false, nil
}
