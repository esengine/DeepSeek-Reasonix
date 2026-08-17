package agent

import (
	"os"
	"strings"
)

const maxDismissedTodoBatches = 160

// RecordDismissedTodoBatch appends a completed todo-list fingerprint to the
// session sidecar so desktop can hide that shelf after an upgrade remount.
func RecordDismissedTodoBatch(sessionPath, batch string) error {
	sessionPath = strings.TrimSpace(sessionPath)
	batch = strings.TrimSpace(batch)
	if sessionPath == "" || batch == "" {
		return nil
	}
	if _, err := os.Stat(sessionPath); err != nil {
		if _, metaErr := os.Stat(BranchMetaPath(sessionPath)); metaErr != nil {
			return nil
		}
	}
	return UpdateBranchMeta(sessionPath, false, func(m *BranchMeta) error {
		m.DismissedTodoBatches = AppendDismissedTodoBatch(m.DismissedTodoBatches, batch)
		return nil
	})
}

func AppendDismissedTodoBatch(existing []string, key string) []string {
	return MergeDismissedTodoBatches(existing, []string{key})
}

func MergeDismissedTodoBatches(sets ...[]string) []string {
	n := 0
	for _, set := range sets {
		n += len(set)
	}
	all := make([]string, 0, n)
	for _, set := range sets {
		all = append(all, set...)
	}
	return NormalizeDismissedTodoBatches(all)
}

func NormalizeDismissedTodoBatches(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) > maxDismissedTodoBatches {
		out = out[len(out)-maxDismissedTodoBatches:]
	}
	return out
}

func DismissedTodoBatches(sessionPath string) []string {
	meta, ok, err := LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return nil
	}
	return NormalizeDismissedTodoBatches(meta.DismissedTodoBatches)
}
