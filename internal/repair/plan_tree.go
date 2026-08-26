package repair

import (
	"fmt"
	"os"
	"path"
	"strings"
)

func repairPlanTreePayloadStateID(root string) (string, error) {
	return repairPlanTreeDigest(root, repairPlanTreePayloadEntries)
}

// AppBundlePayloadTreeDigest is the macOS handoff identity that ignores POSIX
// mode bits and AppleDouble sidecar files.
func AppBundlePayloadTreeDigest(path string) (string, error) {
	return repairPlanTreePayloadStateID(path)
}

func repairPlanTreeDigest(root string, adapt func([]repairPlanTreeEntry) []repairPlanTreeEntry) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("expected directory, got %s", info.Mode().Type())
	}
	entries, err := repairPlanTreeEntries(root)
	if err != nil {
		return "", err
	}
	if adapt != nil {
		entries = adapt(entries)
	}
	for _, entry := range entries {
		switch entry.Kind {
		case "unreadable", "file-unreadable", "symlink-unreadable":
			return "", fmt.Errorf("cannot read bundle entry %q", entry.Rel)
		case "other":
			return "", fmt.Errorf("unsupported bundle entry %q", entry.Rel)
		}
	}
	return repairPlanStateID(entries), nil
}

func repairPlanTreeHandoffAppMatches(path, expected string) (bool, error) {
	payload, err := repairPlanTreePayloadStateID(path)
	if err != nil {
		return false, err
	}
	if payload == expected {
		return true, nil
	}
	strict, err := repairPlanTreeContentStateID(path)
	if err != nil {
		return false, err
	}
	return strict == expected, nil
}

// repairPlanTreePayloadEntries drops POSIX mode bits and AppleDouble sidecar
// files that ditto cannot preserve on volumes such as exFAT.
func repairPlanTreePayloadEntries(entries []repairPlanTreeEntry) []repairPlanTreeEntry {
	have := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		have[entry.Rel] = struct{}{}
	}
	out := make([]repairPlanTreeEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind == "file" && appleDoubleSidecarRel(entry.Rel, have) {
			continue
		}
		entry.Mode = 0
		out = append(out, entry)
	}
	return out
}

func appleDoubleSidecarRel(rel string, have map[string]struct{}) bool {
	base := path.Base(rel)
	if !strings.HasPrefix(base, "._") || base == "._" {
		return false
	}
	sibling := strings.TrimPrefix(base, "._")
	if dir := path.Dir(rel); dir != "." {
		sibling = dir + "/" + sibling
	}
	_, ok := have[sibling]
	return ok
}
