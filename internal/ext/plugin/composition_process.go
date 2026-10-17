package plugin

import (
	"fmt"
	"os"
	"path/filepath"
)

// CompositionProcessRoot prepares a private directory beneath the MCP state owner.
func CompositionProcessRoot(s Spec) (string, error) {
	rel, err := filepath.Rel(filepath.Join(s.StateDir, "sessions"), s.ProcessStateDir)
	if s.StateDir == "" || err != nil || !filepath.IsLocal(rel) || rel == "." || filepath.Base(rel) != rel {
		return "", fmt.Errorf("invalid MCP process state directory")
	}
	for _, dir := range []string{s.StateDir, filepath.Join(s.StateDir, "sessions"), s.ProcessStateDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("MCP process state crosses a symbolic link")
		}
	}
	return s.ProcessStateDir, nil
}

func compositionTransportState(s Spec) string {
	if s.ProcessStateDir != "" && s.Package != "" {
		return filepath.Join(s.ProcessStateDir, "runtime")
	}
	return s.ProcessStateDir
}
