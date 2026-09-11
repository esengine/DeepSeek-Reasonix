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
	paths, err := derivedStateTargetPaths(target)
	if err != nil {
		return nil, err
	}
	_, expectedStates := repairPlanDerivedStateSnapshot(strings.ToLower(strings.TrimSpace(target)))
	unlockTransaction, err := lockRepairTransaction()
	if err != nil {
		return nil, err
	}
	defer unlockTransaction()
	if err := reconcilePreparedRepairTransaction(); err != nil {
		return nil, fmt.Errorf("rebuild derived state: reconcile pending mutation: %w", err)
	}
	unlock, err := lockRepairMutations(paths...)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := verifyRepairPlanFileStates(expectedStates); err != nil {
		return nil, err
	}
	return rebuildDerivedStateBoundUnlocked(target, expectedStates, nil)
}

func rebuildDerivedStateBoundUnlocked(
	target string,
	expectedStates map[string]string,
	planTx *RepairTransaction,
) ([]string, error) {
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
	tx := planTx
	if tx == nil {
		tx = newRepairTransaction(time.Now())
	}
	for _, name := range names {
		path := paths[name]
		if path == "" {
			continue
		}
		if _, err := os.Lstat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return applied, err
		}
		if err := verifyRepairPlanFileState(path, expectedStates); err != nil {
			return applied, err
		}
		repairMutationBeforeRename(path)
		if err := verifyRepairPlanFileState(path, expectedStates); err != nil {
			return applied, err
		}
		quarantine := path + ".reasonix-rebuild-" + stamp
		changeIndex := len(tx.Changes)
		tx.Changes = append(tx.Changes, preparedRepairChangeForPrevious("derived:"+name, path, quarantine))
		if err := persistPreparedRepairTransaction(tx); err != nil {
			return applied, fmt.Errorf("prepare derived-state quarantine: %w", err)
		}
		repairMutationAfterPrepare(path)
		if err := renameRepairNodeNoReplace(path, quarantine); err != nil {
			return applied, err
		}
		repairMutationAfterRename(path)
		if expected := expectedStates[path]; expected != "" {
			if err := verifyRepairPlanStateIDFor(quarantine, path, expected); err != nil {
				if restoreErr := restoreRepairNodeIfAbsent(quarantine, path); restoreErr != nil {
					return applied, fmt.Errorf("derived state changed after confirmation and restore failed: %w: %w", restoreErr, err)
				}
				return applied, err
			}
		}
		if durable, err := commitPreparedRepairTransaction(tx, changeIndex); err != nil {
			if durable {
				return applied, fmt.Errorf("commit derived-state undo state: cleanup pending journal: %w", err)
			}
			restoreErr := restoreRepairNodeIfAbsent(quarantine, path)
			if restoreErr != nil {
				return applied, fmt.Errorf("commit derived-state undo state: %w; confirmed state retained at %s: %w", err, quarantine, restoreErr)
			}
			return applied, fmt.Errorf("commit derived-state undo state: %w", err)
		}
		if _, err := os.Lstat(path); err == nil {
			appendRepairLogBestEffort(tx)
			return applied, fmt.Errorf("repair plan preview changed since confirmation; target was recreated during quarantine; confirmed state remains at %s", quarantine)
		} else if !os.IsNotExist(err) {
			return applied, err
		}
		applied = append(applied, quarantine)
	}
	if len(tx.Changes) > 0 {
		appendRepairLogBestEffort(tx)
	}
	return applied, nil
}

func derivedStateTargetPaths(target string) ([]string, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	paths := derivedStatePaths()
	if target == "all" {
		names := make([]string, 0, len(paths))
		for name := range paths {
			names = append(names, name)
		}
		sort.Strings(names)
		out := make([]string, 0, len(names))
		for _, name := range names {
			if path := paths[name]; path != "" {
				out = append(out, path)
			}
		}
		return out, nil
	}
	path, ok := paths[target]
	if !ok {
		return nil, fmt.Errorf("unknown derived-state target %q (want tabs|projects|window|zoom|all)", target)
	}
	if path == "" {
		return []string{}, nil
	}
	return []string{path}, nil
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
