package agent

import (
	"path/filepath"
	"strings"
)

// RecoveryFilenameParentID recognizes legacy automatic recovery files named
// <stem>-recovery-<16 hex>.jsonl when meta is missing or incomplete. The parent
// ID is the stem before the final -recovery-<hex> suffix (not a content proof).
func RecoveryFilenameParentID(path string) (string, bool) {
	id := BranchID(path)
	parent, ok := peelRecoveryFilenameSuffix(id)
	if !ok {
		return "", false
	}
	return parent, true
}

// RecoveryFilenameRootID peels nested -recovery-<16 hex> suffixes to the
// non-recovery stem used as a stable recovery-group key for old files.
func RecoveryFilenameRootID(path string) (string, bool) {
	id := BranchID(path)
	root, ok := peelRecoveryFilenameSuffix(id)
	if !ok {
		return "", false
	}
	for {
		next, nested := peelRecoveryFilenameSuffix(root)
		if !nested {
			break
		}
		root = next
	}
	if strings.TrimSpace(root) == "" {
		return "", false
	}
	return root, true
}

// LooksLikeRecoveryFilename reports whether the path follows the automatic
// recovery naming convention used before meta always carried Recovered=true.
func LooksLikeRecoveryFilename(path string) bool {
	_, ok := peelRecoveryFilenameSuffix(BranchID(path))
	return ok
}

func peelRecoveryFilenameSuffix(id string) (string, bool) {
	id = strings.TrimSpace(id)
	const marker = "-recovery-"
	index := strings.LastIndex(id, marker)
	if index <= 0 {
		return "", false
	}
	suffix := id[index+len(marker):]
	if len(suffix) != 16 || !isLowerHex(suffix) {
		return "", false
	}
	parent := strings.TrimSpace(id[:index])
	if parent == "" || parent == "." || filepath.Base(parent) != parent {
		return "", false
	}
	return parent, true
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
