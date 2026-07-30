package repair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

type ConfigCheck struct {
	Scope        string `json:"scope"`
	Path         string `json:"path"`
	Exists       bool   `json:"exists"`
	Valid        bool   `json:"valid"`
	Error        string `json:"error,omitempty"`
	SnapshotPath string `json:"snapshotPath,omitempty"`
}

type ConfigReport struct {
	Checks  []ConfigCheck `json:"checks"`
	Applied []string      `json:"applied"`
}

type ConfigOptions struct {
	Root           string
	Apply          bool
	IncludeProject bool
	OnlyScope      string
	Now            func() time.Time

	expectedStates         map[string]string
	confirmedGlobalRestore []byte
	hasConfirmedRestore    bool
	repairTransaction      *RepairTransaction
}

func InspectAndRepairConfig(opts ConfigOptions) (ConfigReport, error) {
	if !opts.Apply {
		return inspectAndRepairConfigUnlocked(opts)
	}
	paths, err := configRepairTargetPaths(opts)
	if err != nil {
		return ConfigReport{}, err
	}
	// Direct callers do not carry an external preview ID. Bind their invocation
	// before waiting on either lock so a newer config or snapshot cannot become
	// the implicit object of an older call.
	if opts.expectedStates == nil {
		opts.expectedStates = make(map[string]string, len(paths))
		for _, path := range paths {
			opts.expectedStates[path] = repairPlanFileState(path)
		}
		if snapshot := lastKnownGoodConfigPath(); snapshot != "" {
			bound := repairPlanFileSnapshotAt(snapshot)
			opts.confirmedGlobalRestore = append([]byte(nil), bound.Content...)
			opts.hasConfirmedRestore = bound.Readable
		}
	}
	unlockTransaction, err := lockRepairTransaction()
	if err != nil {
		return ConfigReport{}, err
	}
	defer unlockTransaction()
	if err := reconcilePreparedRepairTransaction(); err != nil {
		return ConfigReport{}, fmt.Errorf("repair config: reconcile pending mutation: %w", err)
	}
	unlock, err := lockRepairMutations(paths...)
	if err != nil {
		return ConfigReport{}, err
	}
	defer unlock()
	if err := verifyRepairPlanFileStates(opts.expectedStates); err != nil {
		return ConfigReport{}, err
	}
	return inspectAndRepairConfigUnlocked(opts)
}

func inspectAndRepairConfigUnlocked(opts ConfigOptions) (ConfigReport, error) {
	if opts.OnlyScope != "" && opts.OnlyScope != "global" && opts.OnlyScope != "project" {
		return ConfigReport{}, fmt.Errorf("unknown config repair scope %q", opts.OnlyScope)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	global := config.UserConfigPath()
	project := filepath.Join(opts.Root, "reasonix.toml")
	if opts.Root == "" || opts.Root == "." {
		project = "reasonix.toml"
	}
	paths := []struct{ scope, path string }{{"global", global}, {"project", project}}
	report := ConfigReport{Checks: make([]ConfigCheck, 0, len(paths)), Applied: []string{}}
	tx := opts.repairTransaction
	if tx == nil {
		tx = newRepairTransaction(opts.Now())
	}
	for _, item := range paths {
		check := inspectConfig(item.scope, item.path)
		if item.scope == "global" {
			check.SnapshotPath = lastKnownGoodConfigPath()
		}
		report.Checks = append(report.Checks, check)
		if !opts.Apply || !check.Exists || check.Valid || (opts.OnlyScope != "" && item.scope != opts.OnlyScope) || (item.scope == "project" && !opts.IncludeProject) {
			continue
		}
		if err := verifyRepairPlanFileState(item.path, opts.expectedStates); err != nil {
			return report, err
		}
		if item.scope == "global" {
			if err := verifyRepairPlanFileState(lastKnownGoodConfigPath(), opts.expectedStates); err != nil {
				return report, err
			}
		}
		repairMutationBeforeRename(item.path)
		if err := verifyRepairPlanFileState(item.path, opts.expectedStates); err != nil {
			return report, err
		}
		if item.scope == "global" {
			if err := verifyRepairPlanFileState(lastKnownGoodConfigPath(), opts.expectedStates); err != nil {
				return report, err
			}
		}
		quarantine := item.path + ".reasonix-quarantine-" + opts.Now().UTC().Format("20060102T150405Z")
		changeIndex := len(tx.Changes)
		tx.Changes = append(tx.Changes, preparedRepairChangeForPrevious(item.scope, item.path, quarantine))
		if err := persistPreparedRepairTransaction(tx); err != nil {
			return report, fmt.Errorf("prepare quarantine %s config: %w", item.scope, err)
		}
		repairMutationAfterPrepare(item.path)
		if err := renameRepairNodeNoReplace(item.path, quarantine); err != nil {
			return report, fmt.Errorf("quarantine %s config: %w", item.scope, err)
		}
		repairMutationAfterRename(item.path)
		if expected := opts.expectedStates[item.path]; expected != "" {
			if err := verifyRepairPlanStateIDFor(quarantine, item.path, expected); err != nil {
				if restoreErr := restoreRepairNodeIfAbsent(quarantine, item.path); restoreErr != nil {
					return report, fmt.Errorf("quarantine %s config changed after confirmation and restore failed: %v: %w", item.scope, restoreErr, err)
				}
				return report, err
			}
		}
		if durable, err := commitPreparedRepairTransaction(tx, changeIndex); err != nil {
			if durable {
				return report, fmt.Errorf("commit quarantine %s config undo state: cleanup pending journal: %w", item.scope, err)
			}
			restoreErr := restoreRepairNodeIfAbsent(quarantine, item.path)
			if restoreErr != nil {
				return report, fmt.Errorf("commit quarantine %s config undo state: %w; confirmed config retained at %s: %v", item.scope, err, quarantine, restoreErr)
			}
			return report, fmt.Errorf("commit quarantine %s config undo state: %w", item.scope, err)
		}
		if _, err := os.Lstat(item.path); err == nil {
			appendRepairLogBestEffort(tx)
			return report, fmt.Errorf("repair plan preview changed since confirmation; target was recreated during quarantine; confirmed state remains at %s", quarantine)
		} else if !os.IsNotExist(err) {
			return report, err
		}
		report.Applied = append(report.Applied, "quarantined "+item.scope+" config at "+quarantine)
		if item.scope == "global" {
			restoreErr := os.ErrNotExist
			if opts.expectedStates != nil {
				if opts.hasConfirmedRestore {
					if restoreErr = config.ValidateBytes(opts.confirmedGlobalRestore); restoreErr == nil {
						restoreErr = fileutil.AtomicCreateFile(item.path, opts.confirmedGlobalRestore, 0o600)
					}
				}
			} else {
				restoreErr = restoreLastKnownGoodConfig(item.path)
			}
			if restoreErr == nil {
				report.Applied = append(report.Applied, "restored global config from last-known-good snapshot")
			} else if opts.expectedStates != nil && opts.hasConfirmedRestore {
				return report, fmt.Errorf("restore confirmed last-known-good config: %w", restoreErr)
			}
		}
		report.Checks[len(report.Checks)-1] = inspectConfig(item.scope, item.path)
		if item.scope == "global" {
			report.Checks[len(report.Checks)-1].SnapshotPath = lastKnownGoodConfigPath()
		}
	}
	if len(tx.Changes) > 0 {
		appendRepairLogBestEffort(tx)
	}
	return report, nil
}

func configRepairTargetPaths(opts ConfigOptions) ([]string, error) {
	if opts.OnlyScope != "" && opts.OnlyScope != "global" && opts.OnlyScope != "project" {
		return nil, fmt.Errorf("unknown config repair scope %q", opts.OnlyScope)
	}
	globalPaths := func() []string {
		paths := []string{config.UserConfigPath()}
		if snapshot := lastKnownGoodConfigPath(); snapshot != "" {
			paths = append(paths, snapshot)
		}
		return paths
	}
	project := filepath.Join(opts.Root, "reasonix.toml")
	if opts.Root == "" || opts.Root == "." {
		project = "reasonix.toml"
	}
	switch opts.OnlyScope {
	case "global":
		return globalPaths(), nil
	case "project":
		return []string{project}, nil
	default:
		paths := globalPaths()
		if opts.IncludeProject {
			paths = append(paths, project)
		}
		return paths, nil
	}
}

func inspectConfig(scope, path string) ConfigCheck {
	check := ConfigCheck{Scope: scope, Path: path, Valid: true}
	if path == "" {
		return check
	}
	if _, err := os.Lstat(path); err != nil {
		if !os.IsNotExist(err) {
			check.Valid = false
			check.Error = err.Error()
		}
		return check
	}
	check.Exists = true
	b, err := os.ReadFile(path)
	if err == nil {
		err = config.ValidateBytes(b)
	}
	if err != nil {
		check.Valid = false
		check.Error = err.Error()
	}
	return check
}

type snapshotMeta struct {
	SchemaVersion int    `json:"schemaVersion"`
	SourcePath    string `json:"sourcePath"`
	RecordedAt    string `json:"recordedAt"`
	Version       string `json:"version,omitempty"`
}

func RecordHealthyConfig(version string) error {
	path := config.UserConfigPath()
	if path == "" {
		return nil
	}
	snapshot := lastKnownGoodConfigPath()
	if snapshot == "" {
		return nil
	}
	unlock, err := lockRepairMutations(path, snapshot, snapshot+".json", snapshotDir())
	if err != nil {
		return err
	}
	defer unlock()

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := config.ValidateBytes(b); err != nil {
		return err
	}
	now := time.Now().UTC()
	meta := snapshotMeta{SchemaVersion: 1, SourcePath: path, RecordedAt: now.Format(time.RFC3339Nano), Version: version}
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	// Publish the immutable, versioned recovery point first. If a later fixed
	// last-known-good write fails, readers retain the previous fixed snapshot
	// while the newly recorded version remains independently recoverable.
	if err := recordConfigSnapshot(path, b, version, now); err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(snapshot, b, 0o600); err != nil {
		return err
	}
	// The metadata is informational; restore consumes and validates only the
	// content file. Both writers are serialized by the same mutation locks, so
	// a failed metadata replacement cannot expose unverified recovery bytes.
	if err := fileutil.AtomicWriteFile(snapshot+".json", append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	return nil
}

func lastKnownGoodConfigPath() string {
	root := config.MemoryUserDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "repair", "config.toml.last-known-good")
}

func restoreLastKnownGoodConfig(dest string) error {
	snapshot := lastKnownGoodConfigPath()
	b, err := os.ReadFile(snapshot)
	if err != nil {
		return err
	}
	if err := config.ValidateBytes(b); err != nil {
		return err
	}
	return fileutil.AtomicCreateFile(dest, b, 0o600)
}
