package workspaceid

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// PathFingerprint is the digest of a workspace's resolved absolute path, the
// key MCP launch authorizations and legacy capability activations are filed
// under. Unlike Key it does not follow a repository: two worktrees differ.
func PathFingerprint(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:])
}
