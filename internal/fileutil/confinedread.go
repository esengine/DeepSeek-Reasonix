package fileutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

func confinedRelativePath(rel string) (string, error) {
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "" || rel == "." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative to the workspace")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the workspace")
	}
	return rel, nil
}
