//go:build !linux

package lifecycle

import (
	"fmt"
	"os"
)

func openCurrentExecutable() (*os.File, os.FileInfo, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve current executable: %w", err)
	}
	return openExplicitExecutable(path)
}
