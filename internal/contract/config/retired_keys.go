// retired_keys.go — stripping config keys the runtime no longer honors.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// MigrateLegacyAgentStepLimitsForRoot removes retired [agent] step-limit keys
// from the user and project config selected for root. Boot calls it immediately
// before LoadForRoot, so config-only/read-only commands never rewrite files and
// the runtime can surface exactly one migration notice.
func MigrateLegacyAgentStepLimitsForRoot(root string) (bool, error) {
	return processRoots().MigrateLegacyAgentStepLimitsForRoot(root)
}

// MigrateLegacyAgentStepLimitsForRoot removes retired [agent] step-limit keys from this binding's
func (r Roots) MigrateLegacyAgentStepLimitsForRoot(root string) (bool, error) {
	root = resolveRoot(root)
	paths := make([]string, 0, 2)
	if userPath := r.userConfigLoadPath(); userPath != "" {
		paths = append(paths, userPath)
	}
	paths = append(paths, ProjectConfigPath(root))

	changedAny := false
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		changed, err := migrateLegacyAgentStepLimitsFile(path)
		if err != nil {
			return changedAny, fmt.Errorf("migrate deprecated agent step limits in %s: %w", path, err)
		}
		changedAny = changedAny || changed
	}
	return changedAny, nil
}

// migrateLegacyAgentStepLimitsFile removes retired [agent] step-limit keys
// before runtime decoding. A process-wide lock makes concurrent desktop tab
// builds observe a single migration; the atomic rewrite protects other readers.
func migrateLegacyAgentStepLimitsFile(path string) (bool, error) {
	return migrateRetiredConfigKeysFile(path, stripLegacyAgentStepLimitLines)
}

func stripLegacyAgentStepLimitLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "agent", "max_steps", "planner_max_steps")
}

// MigrateLegacyRedactToolOutputForRoot removes the retired
// [secrets].redact_tool_output setting from the user and project configs chosen
// for root. The setting no longer controls any runtime behavior; removing it
// avoids leaving an explicit `true` value on disk that falsely suggests live
// output or transcript redaction is still active.
func MigrateLegacyRedactToolOutputForRoot(root string) (bool, error) {
	return processRoots().MigrateLegacyRedactToolOutputForRoot(root)
}

// MigrateLegacyRedactToolOutputForRoot removes the retired redact_tool_output key from this binding's
func (r Roots) MigrateLegacyRedactToolOutputForRoot(root string) (bool, error) {
	root = resolveRoot(root)
	paths := make([]string, 0, 2)
	if userPath := r.userConfigLoadPath(); userPath != "" {
		paths = append(paths, userPath)
	}
	paths = append(paths, ProjectConfigPath(root))

	changedAny := false
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		changed, err := migrateLegacyRedactToolOutputFile(path)
		if err != nil {
			return changedAny, fmt.Errorf("migrate deprecated redact_tool_output in %s: %w", path, err)
		}
		changedAny = changedAny || changed
	}
	return changedAny, nil
}

func migrateLegacyRedactToolOutputFile(path string) (bool, error) {
	return migrateRetiredConfigKeysFile(path, stripLegacyRedactToolOutputLines)
}

func stripLegacyRedactToolOutputLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "secrets", "redact_tool_output")
}

// MigrateLegacyMemoryCompilerForRoot removes the retired
// [agent].memory_compiler setting from the user and project configs chosen for
// root. The Memory v5 execution compiler was removed; stripping the key avoids
// leaving values on disk that falsely suggest compiler behavior (especially a
// stale verbosity = "compact") is still active.
func MigrateLegacyMemoryCompilerForRoot(root string) (bool, error) {
	return processRoots().MigrateLegacyMemoryCompilerForRoot(root)
}

// MigrateLegacyMemoryCompilerForRoot removes the retired memory_compiler key from this binding's
func (r Roots) MigrateLegacyMemoryCompilerForRoot(root string) (bool, error) {
	root = resolveRoot(root)
	paths := make([]string, 0, 2)
	if userPath := r.userConfigLoadPath(); userPath != "" {
		paths = append(paths, userPath)
	}
	paths = append(paths, ProjectConfigPath(root))

	changedAny := false
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		changed, err := migrateLegacyMemoryCompilerFile(path)
		if err != nil {
			return changedAny, fmt.Errorf("migrate deprecated memory_compiler in %s: %w", path, err)
		}
		changedAny = changedAny || changed
	}
	return changedAny, nil
}

func migrateLegacyMemoryCompilerFile(path string) (bool, error) {
	return migrateRetiredConfigKeysFile(path, stripLegacyMemoryCompilerLines)
}

func migrateRetiredConfigKeysFile(path string, strip func(string) (string, bool)) (bool, error) {
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return false, err
	}
	defer unlock()
	resolved, exists, err := statConfigPath(path)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return false, err
	}
	raw, err := fileencoding.ReadFileUTF8(resolved)
	if err != nil {
		return false, err
	}
	next, changed := strip(string(raw))
	if !changed {
		return false, nil
	}
	if err := fileutil.AtomicWriteFile(resolved, []byte(next), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func stripLegacyMemoryCompilerLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "agent", "memory_compiler")
}

// MigrateLegacyMultiThresholdCompactionForRoot strips retired soft/snip/force keys.
func MigrateLegacyMultiThresholdCompactionForRoot(root string) (bool, error) {
	return processRoots().MigrateLegacyMultiThresholdCompactionForRoot(root)
}

// MigrateLegacyMultiThresholdCompactionForRoot strips retired compaction threshold keys from this binding's
func (r Roots) MigrateLegacyMultiThresholdCompactionForRoot(root string) (bool, error) {
	root = resolveRoot(root)
	paths := make([]string, 0, 2)
	if userPath := r.userConfigLoadPath(); userPath != "" {
		paths = append(paths, userPath)
	}
	paths = append(paths, ProjectConfigPath(root))

	changedAny := false
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		changed, err := migrateLegacyMultiThresholdCompactionFile(path)
		if err != nil {
			return changedAny, fmt.Errorf("migrate deprecated multi-threshold compaction keys in %s: %w", path, err)
		}
		changedAny = changedAny || changed
	}
	return changedAny, nil
}

func migrateLegacyMultiThresholdCompactionFile(path string) (bool, error) {
	return migrateRetiredConfigKeysFile(path, stripLegacyMultiThresholdCompactionLines)
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
