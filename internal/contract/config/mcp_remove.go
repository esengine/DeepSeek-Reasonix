package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RemovePluginFromEffectiveSourceForRoot removes only the declaration currently
// selected by the project-over-global precedence rules.
// The returned entry is empty only when no declaration remains effective.
func RemovePluginFromEffectiveSourceForRoot(root, name string) (PluginEntry, bool, string, error) {
	unlock, err := LockConfigFilesEdits(mcpConfigSourcePathsForRoot(root)...)
	if err != nil {
		return PluginEntry{}, false, "", fmt.Errorf("remove effective MCP server: %w", err)
	}
	defer unlock()

	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		return PluginEntry{}, false, "", err
	}
	if err := validateMCPRemovalSources(root, cfg); err != nil {
		return PluginEntry{}, false, "", err
	}
	entry, found := pluginEntryByName(cfg.Plugins, strings.TrimSpace(name))
	if !found {
		return PluginEntry{}, false, "", nil
	}
	path := MCPConfigPathForEntry(root, entry)
	removed, path, err := removePluginFromSourceForRootLocked(entry, path)
	return entry, removed, path, err
}

func validateMCPRemovalSources(root string, cfg *Config) error {
	if _, err := loadClaudeMCPStrict(processRoots().claudeConfigPath(), root); err != nil {
		return err
	}
	if !processRoots().mcpGlobalMigrationComplete() {
		if _, err := loadLegacyMCPStrict(legacyConfigPath()); err != nil {
			return err
		}
	}
	if !cfg.HasLoadWarnings() {
		return nil
	}
	// A recovered config can omit a declaration that still exists on disk.
	// Only a strict source read can distinguish that from an external removal.
	for _, path := range []string{processRoots().userConfigLoadPath(), ProjectConfigPath(root)} {
		if err := ValidateFile(path); err != nil {
			return err
		}
	}
	_, err := loadMCPJSON(filepath.Join(resolveRoot(root), mcpJSONFile))
	return err
}
