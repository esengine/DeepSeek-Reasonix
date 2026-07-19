package repair

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
)

func RebuildDerivedState(target string) ([]string, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	paths := derivedStatePaths()
	var names []string
	if target == "all" {
		for name := range paths {
			names = append(names, name)
		}
		sort.Strings(names)
	} else if _, ok := paths[target]; ok {
		names = []string{target}
	} else {
		return nil, fmt.Errorf("unknown derived-state target %q (want tabs|projects|window|zoom|all)", target)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	applied := []string{}
	tx := newRepairTransaction(time.Now())
	for _, name := range names {
		path := paths[name]
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return applied, err
		}
		quarantine := path + ".reasonix-rebuild-" + stamp
		if err := os.Rename(path, quarantine); err != nil {
			return applied, err
		}
		applied = append(applied, quarantine)
		tx.Changes = append(tx.Changes, RepairChange{Scope: "derived:" + name, TargetPath: path, PreviousPath: quarantine})
		if err := persistRepairTransaction(tx); err != nil {
			_ = os.Rename(quarantine, path)
			return applied, err
		}
	}
	if len(tx.Changes) > 0 {
		appendRepairLogBestEffort(tx)
	}
	return applied, nil
}

func derivedStatePaths() map[string]string {
	paths := map[string]string{}
	if root := config.ReasonixHomeDir(); root != "" {
		paths["tabs"] = filepath.Join(root, "desktop-tabs.json")
		paths["projects"] = filepath.Join(root, "desktop-projects.json")
	}
	if root := config.MemoryUserDir(); root != "" {
		paths["window"] = filepath.Join(root, "desktop-window.json")
		paths["zoom"] = filepath.Join(root, "desktop-zoom.json")
	}
	return paths
}
