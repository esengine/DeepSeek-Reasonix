package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
)

type executableSourceOpener func() (*os.File, os.FileInfo, error)

func (m *SystemdManager) openExecutableSource() (*os.File, os.FileInfo, error) {
	if m == nil || m.sourceOpener == nil {
		return nil, nil, fmt.Errorf("executable source opener is unavailable: %w", ErrUnsafeArtifact)
	}
	return m.sourceOpener()
}

func openExplicitExecutable(path string) (*os.File, os.FileInfo, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve explicit executable test source: %w", err)
	}
	before, err := os.Lstat(resolved)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect explicit executable test source: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("resolved explicit executable test source %q is not regular: %w", resolved, ErrUnsafeArtifact)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, nil, fmt.Errorf("open explicit executable test source: %w", err)
	}
	after, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Close()
		return nil, nil, fmt.Errorf("explicit executable test source changed while opening: %w", ErrUnsafeArtifact)
	}
	return file, after, nil
}
