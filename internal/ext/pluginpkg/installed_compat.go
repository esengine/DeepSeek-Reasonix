package pluginpkg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// LoadInstalled reads one snapshot of the state file and parses the enabled
// packages from it with no lock held, so a slow disk stalls only this caller.
// The result describes the snapshot: an install that lands mid-parse shows up
// in the next call, exactly as if this call had finished first.
func LoadInstalled(reasonixHome string) ([]InstalledPackage, []string) {
	stateMu.Lock()
	st, err := LoadState(reasonixHome)
	stateMu.Unlock()
	if err != nil {
		return nil, []string{err.Error()}
	}
	var out []InstalledPackage
	var warnings []string
	var verdicts []statusVerdict
	for _, installed := range st.Plugins {
		if !installed.Enabled {
			continue
		}
		root := ResolveRoot(reasonixHome, installed.Root)
		pkg, pkgWarnings, err := ParseDir(root)
		if err != nil && errors.Is(err, ErrMissingAPIVersion) && managedPluginRoot(reasonixHome, installed, root) {
			var migrated bool
			pkg, pkgWarnings, migrated, err = migrateManagedManifest(root)
			if migrated {
				warnings = append(warnings, fmt.Sprintf("%s: automatically migrated managed manifest to %s (backup: %s.bak)", installed.Name, ManifestAPIVersionV2, NativeManifest))
			}
		}
		if err != nil {
			reason := err.Error()
			if installed.Status != PluginStatusDisabledIncompatible || installed.StatusReason != reason {
				verdicts = append(verdicts, statusVerdict{installed: installed, status: PluginStatusDisabledIncompatible, reason: reason})
			}
			remediation := "repair or reinstall the plugin"
			if errors.Is(err, ErrMissingAPIVersion) {
				remediation = fmt.Sprintf("reasonix plugin migrate %s --to-v2", installed.Name)
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s: %v; remediation: %s", installed.Name, PluginStatusDisabledIncompatible, err, remediation))
			continue
		}
		if installed.Status != "" || installed.StatusReason != "" {
			verdicts = append(verdicts, statusVerdict{installed: installed})
		}
		out = append(out, InstalledPackage{Installed: installed, Package: pkg, Warnings: pkgWarnings})
	}
	if err := persistStatusVerdicts(reasonixHome, verdicts); err != nil {
		warnings = append(warnings, "persist plugin compatibility status: "+err.Error())
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Installed.Name < out[j].Installed.Name })
	return out, warnings
}

// migrateLock keeps two loaders from rewriting the same manifest at once; the
// loser re-parses and finds the winner's result.
var migrateLock sync.Mutex

func migrateManagedManifest(root string) (Package, []string, bool, error) {
	migrateLock.Lock()
	defer migrateLock.Unlock()
	if pkg, warnings, err := ParseDir(root); !errors.Is(err, ErrMissingAPIVersion) {
		return pkg, warnings, false, err
	}
	legacy, _, migrateErr := ParseNativeForMigrate(root)
	if migrateErr == nil {
		var data []byte
		data, migrateErr = MigrateManifestToV2(legacy)
		if migrateErr == nil {
			migrateErr = WriteMigratedManifestV2(root, data)
		}
	}
	if migrateErr != nil {
		return Package{}, nil, false, fmt.Errorf("automatic managed-plugin migration failed: %w", migrateErr)
	}
	pkg, warnings, err := ParseDir(root)
	return pkg, warnings, err == nil, err
}

// statusVerdict is a compatibility status computed against a snapshot entry.
type statusVerdict struct {
	installed      InstalledPlugin
	status, reason string
}

// persistStatusVerdicts applies verdicts to the current state, skipping any
// entry that was removed, reinstalled or disabled since the snapshot: a verdict
// about an install that is no longer the entry says nothing about it.
func persistStatusVerdicts(reasonixHome string, verdicts []statusVerdict) error {
	if len(verdicts) == 0 {
		return nil
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	st, err := LoadState(reasonixHome)
	if err != nil {
		return err
	}
	changed := false
	for _, v := range verdicts {
		for i := range st.Plugins {
			cur := &st.Plugins[i]
			if cur.Name != v.installed.Name || !cur.Enabled || cur.Root != v.installed.Root || cur.Generation != v.installed.Generation {
				continue
			}
			if cur.Status != v.status || cur.StatusReason != v.reason {
				cur.Status, cur.StatusReason = v.status, v.reason
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	return SaveState(reasonixHome, st)
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
