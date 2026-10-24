package store

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// SessionEnvironment keeps environment metadata addressable even for legacy
// transcript basenames close to the filesystem's component-length limit.
func SessionEnvironment(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	name := filepath.Base(sessionPath)
	if len(name) > 180 {
		return filepath.Join(filepath.Dir(sessionPath), fmt.Sprintf(".session-%x.environment-v1", sha256.Sum256([]byte(name))))
	}
	return sessionPath + ".environment-v1"
}
