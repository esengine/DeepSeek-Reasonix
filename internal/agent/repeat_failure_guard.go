package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// repeatFailureBreakThreshold is how many identical write-like failures are
// allowed before refusing the next attempt. Reads do not renew this budget
// because they cannot make unchanged stale write arguments valid.
const repeatFailureBreakThreshold = 2

func (a *Agent) repeatedFailureBlock(call provider.ToolCall, t tool.Tool) (string, bool) {
	sig, _, ok := a.repeatFailureSignature(call, t)
	if !ok || a.repeatFailureCounts == nil {
		return "", false
	}
	record, ok := a.repeatFailureCounts[sig]
	if !ok || record.count < repeatFailureBreakThreshold {
		return "", false
	}
	if repeatFailurePreviewRechecksState(call.Name, record.errClass) {
		if previewer, ok := t.(tool.Previewer); ok {
			_, err := previewer.Preview(json.RawMessage(call.Arguments))
			if err == nil || repeatFailureErrorClass(call.Name, err) != record.errClass {
				delete(a.repeatFailureCounts, sig)
				return "", false
			}
		}
	}
	if record.stateRecheck {
		return fmt.Sprintf(
			"blocked: [loop guard] %q has already failed %d times while the same write intent remained invalid with the same failure class. Re-reading alone cannot make the same stale anchor succeed. Rebuild the edit from the current file contents with a new old_string, use multi_edit for related changes, or explain the blocker in your final answer.",
			call.Name, record.count), true
	}
	return fmt.Sprintf(
		"blocked: [loop guard] %q has already failed %d times with the same write intent and failure class. Change the write conditions or use a different approach instead of retrying the same call, or explain the blocker in your final answer.",
		call.Name, record.count), true
}

func (a *Agent) recordRepeatFailure(call provider.ToolCall, t tool.Tool, execErr error) {
	sig, paths, ok := a.repeatFailureSignature(call, t)
	if !ok {
		return
	}
	if a.repeatFailureCounts == nil {
		a.repeatFailureCounts = make(map[string]repeatFailureRecord)
	}
	errClass := repeatFailureErrorClass(call.Name, execErr)
	record := a.repeatFailureCounts[sig]
	if record.errClass != errClass {
		record = repeatFailureRecord{
			errClass:     errClass,
			paths:        paths,
			stateRecheck: repeatFailurePreviewRechecksState(call.Name, errClass),
		}
	}
	record.count++
	a.repeatFailureCounts[sig] = record
}

func (a *Agent) repeatFailureSignature(call provider.ToolCall, t tool.Tool) (string, []string, bool) {
	if t.ReadOnly() {
		return "", nil, false
	}

	var semantic any
	switch call.Name {
	case "edit_file":
		var p struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &p); err != nil {
			return "", nil, false
		}
		paths := a.normalizeRepeatFailurePaths([]string{p.Path})
		if len(paths) > 0 {
			p.Path = paths[0]
		}
		semantic = p
	case "multi_edit":
		var p struct {
			Path  string `json:"path"`
			Edits []struct {
				OldString  string `json:"old_string"`
				ReplaceAll bool   `json:"replace_all,omitempty"`
			} `json:"edits"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &p); err != nil {
			return "", nil, false
		}
		paths := a.normalizeRepeatFailurePaths([]string{p.Path})
		if len(paths) > 0 {
			p.Path = paths[0]
		}
		semantic = p
	default:
		sig, ok := repeatSuccessSignature(call, t)
		if !ok {
			return "", nil, false
		}
		rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), false, t.ReadOnly())
		return sig, a.normalizeRepeatFailurePaths(rec.Paths), true
	}

	encoded, err := json.Marshal(semantic)
	if err != nil {
		return "", nil, false
	}
	rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), false, t.ReadOnly())
	return call.Name + "\x00" + string(encoded), a.normalizeRepeatFailurePaths(rec.Paths), true
}

func repeatFailureErrorClass(name string, execErr error) string {
	if execErr == nil {
		return ""
	}
	msg := firstLine(execErr.Error())
	switch name {
	case "edit_file", "multi_edit":
		switch {
		case strings.Contains(msg, "old_string not found"):
			return "old_string_not_found"
		case strings.Contains(msg, "old_string is not unique"):
			return "old_string_not_unique"
		}
	}
	return msg
}

func repeatFailurePreviewRechecksState(name, errClass string) bool {
	switch name {
	case "edit_file", "multi_edit":
		return errClass == "old_string_not_found" ||
			errClass == "old_string_not_unique"
	default:
		return false
	}
}

func (a *Agent) clearRepeatFailuresAfterMutation(toolName string, args json.RawMessage, readOnly bool) {
	if len(a.repeatFailureCounts) == 0 {
		return
	}
	rec := evidence.ReceiptFromToolCall(toolName, args, true, readOnly)
	mutatedPaths := a.normalizeRepeatFailurePaths(rec.Paths)
	if len(mutatedPaths) == 0 {
		for sig, failure := range a.repeatFailureCounts {
			// Anchor errors have an exact, side-effect-free state check. Keep
			// their history until Preview shows that the old anchor works again.
			if !failure.stateRecheck {
				delete(a.repeatFailureCounts, sig)
			}
		}
		return
	}
	for sig, failure := range a.repeatFailureCounts {
		// A different edit to the same file does not make this old anchor valid.
		// repeatedFailureBlock rechecks the actual call through Preview.
		if !failure.stateRecheck && repeatFailurePathsOverlap(failure.paths, mutatedPaths) {
			delete(a.repeatFailureCounts, sig)
		}
	}
}

func (a *Agent) normalizeRepeatFailurePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) && a.writeWorkspaceRoot != "" {
			path = filepath.Join(a.writeWorkspaceRoot, path)
		} else if !filepath.IsAbs(path) {
			if absolute, err := filepath.Abs(path); err == nil {
				path = absolute
			}
		}
		path = foldPathKey(filepath.Clean(path))
		if seen[path] {
			continue
		}
		seen[path] = true
		normalized = append(normalized, path)
	}
	return normalized
}

func repeatFailurePathsOverlap(left, right []string) bool {
	for _, a := range left {
		for _, b := range right {
			if pathWithinFold(a, b) || pathWithinFold(b, a) {
				return true
			}
		}
	}
	return false
}
