package pluginpkg

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func LoadInstalled(reasonixHome string) ([]InstalledPackage, []string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	st, err := LoadState(reasonixHome)
	if err != nil {
		return nil, []string{err.Error()}
	}
	var out []InstalledPackage
	var warnings []string
	stateChanged := false
	for i := range st.Plugins {
		installed := st.Plugins[i]
		if !installed.Enabled {
			continue
		}
		root := ResolveRoot(reasonixHome, installed.Root)
		pkg, pkgWarnings, err := ParseDir(root)
		if err != nil && strings.Contains(err.Error(), "missing apiVersion") && managedPluginRoot(reasonixHome, installed, root) {
			legacy, _, migrateErr := ParseNativeForMigrate(root)
			if migrateErr == nil {
				var data []byte
				data, migrateErr = MigrateManifestToV2(legacy)
				if migrateErr == nil {
					migrateErr = WriteMigratedManifestV2(root, data)
				}
			}
			if migrateErr == nil {
				pkg, pkgWarnings, err = ParseDir(root)
				if err == nil {
					warnings = append(warnings, fmt.Sprintf("%s: automatically migrated managed manifest to %s (backup: %s.bak)", installed.Name, ManifestAPIVersionV2, NativeManifest))
				}
			} else {
				err = fmt.Errorf("automatic managed-plugin migration failed: %w", migrateErr)
			}
		}
		if err != nil {
			reason := err.Error()
			if st.Plugins[i].Status != PluginStatusDisabledIncompatible || st.Plugins[i].StatusReason != reason {
				st.Plugins[i].Status = PluginStatusDisabledIncompatible
				st.Plugins[i].StatusReason = reason
				stateChanged = true
			}
			remediation := "repair or reinstall the plugin"
			if strings.Contains(reason, "missing apiVersion") {
				remediation = fmt.Sprintf("reasonix plugin migrate %s --to-v2", installed.Name)
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s: %v; remediation: %s", installed.Name, PluginStatusDisabledIncompatible, err, remediation))
			continue
		}
		if st.Plugins[i].Status != "" || st.Plugins[i].StatusReason != "" {
			st.Plugins[i].Status = ""
			st.Plugins[i].StatusReason = ""
			stateChanged = true
		}
		out = append(out, InstalledPackage{Installed: installed, Package: pkg, Warnings: pkgWarnings})
	}
	if stateChanged {
		if err := SaveState(reasonixHome, st); err != nil {
			warnings = append(warnings, "persist plugin compatibility status: "+err.Error())
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Installed.Name < out[j].Installed.Name })
	return out, warnings
}

func managedPluginRoot(reasonixHome string, installed InstalledPlugin, root string) bool {
	if filepath.IsAbs(strings.TrimSpace(installed.Root)) {
		return false
	}
	managed := filepath.Clean(PluginsDir(reasonixHome))
	rel, err := filepath.Rel(managed, filepath.Clean(root))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	current := managed
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	info, err := os.Lstat(filepath.Join(root, NativeManifest))
	return err == nil && info.Mode()&os.ModeSymlink == 0
}
