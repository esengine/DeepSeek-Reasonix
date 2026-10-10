// retired_keys.go — stripping config keys the runtime no longer honors.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// retiredKey is one config key set the runtime no longer honors and how to
// strip it from a config's text.
type retiredKey struct {
	what  string
	strip func(string) (string, bool)
}

var (
	retiredStepLimits       = retiredKey{"deprecated agent step limits", stripLegacyAgentStepLimitLines}
	retiredRedactToolOutput = retiredKey{"deprecated redact_tool_output", stripLegacyRedactToolOutputLines}
	retiredMemoryCompiler   = retiredKey{"deprecated memory_compiler", stripLegacyMemoryCompilerLines}
	retiredMultiThreshold   = retiredKey{"deprecated multi-threshold compaction keys", stripLegacyMultiThresholdCompactionLines}
)

// RetiredKeyResult is what one retired-key migration did: whether it removed
// anything, and the error that stopped it.
type RetiredKeyResult struct {
	Changed bool
	Err     error
}

// RetiredKeyMigrations holds one result per retired key set.
type RetiredKeyMigrations struct {
	StepLimits       RetiredKeyResult
	RedactToolOutput RetiredKeyResult
	MemoryCompiler   RetiredKeyResult
	MultiThreshold   RetiredKeyResult
}

// MigrateRetiredKeysForRoot removes the retired agent step-limit, redact_tool_output,
// memory_compiler and multi-threshold compaction keys from the user and project
// config selected for root, reading and rewriting each file at most once. Boot
// calls it immediately before LoadForRoot, so config-only/read-only commands
// never rewrite files and the runtime can surface exactly one migration notice.
func (r Roots) MigrateRetiredKeysForRoot(root string) RetiredKeyMigrations {
	res := r.migrateRetiredKeys(root, fileencoding.ReadFileUTF8, []retiredKey{retiredStepLimits, retiredRedactToolOutput, retiredMemoryCompiler, retiredMultiThreshold})
	return RetiredKeyMigrations{StepLimits: res[0], RedactToolOutput: res[1], MemoryCompiler: res[2], MultiThreshold: res[3]}
}

// migrateRetiredKeys runs migs over the user and project config. A migration
// that fails on a file stops there and skips the remaining files, without
// holding back the others.
func (r Roots) migrateRetiredKeys(root string, read func(string) ([]byte, error), migs []retiredKey) []RetiredKeyResult {
	root = resolveRoot(root)
	paths := make([]string, 0, 2)
	if userPath := r.userConfigLoadPath(); userPath != "" {
		paths = append(paths, userPath)
	}
	paths = append(paths, ProjectConfigPath(root))

	results := make([]RetiredKeyResult, len(migs))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		active := make([]retiredKey, 0, len(migs))
		index := make([]int, 0, len(migs))
		for i, m := range migs {
			if results[i].Err == nil {
				active = append(active, m)
				index = append(index, i)
			}
		}
		if len(active) == 0 {
			break
		}
		changed, err := migrateRetiredKeysFile(path, read, active)
		for j, i := range index {
			switch {
			case err == nil:
				results[i].Changed = results[i].Changed || changed[j]
			case changed == nil || changed[j]:
				results[i].Err = fmt.Errorf("migrate %s in %s: %w", migs[i].what, path, err)
			}
		}
	}
	return results
}

// migrateRetiredKeysFile applies migs to one file under its edit lock with a
// single read and at most one atomic write. On an error before the write
// changed is nil and the error concerns every migration; on a write error
// changed names the migrations whose edit was lost.
func migrateRetiredKeysFile(path string, read func(string) ([]byte, error), migs []retiredKey) (changed []bool, err error) {
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	resolved, exists, err := statConfigPath(path)
	if err != nil {
		return nil, err
	}
	changed = make([]bool, len(migs))
	if !exists {
		return changed, nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	raw, err := read(resolved)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	dirty := false
	for i, m := range migs {
		var did bool
		text, did = m.strip(text)
		changed[i] = did
		dirty = dirty || did
	}
	if !dirty {
		return changed, nil
	}
	if err := fileutil.AtomicWriteFile(resolved, []byte(text), info.Mode().Perm()); err != nil {
		return changed, err
	}
	return changed, nil
}

func stripLegacyAgentStepLimitLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "agent", "max_steps", "planner_max_steps")
}

func stripLegacyRedactToolOutputLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "secrets", "redact_tool_output")
}

func stripLegacyMemoryCompilerLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "agent", "memory_compiler")
}

func stripLegacyMultiThresholdCompactionLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "agent",
		"soft_compact_ratio",
		"tool_result_snip_ratio",
		"compact_force_ratio",
		"cold_resume_prune",
		"context_editing",
	)
}
