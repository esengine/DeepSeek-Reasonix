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
}

func InspectAndRepairConfig(opts ConfigOptions) (ConfigReport, error) {
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
	tx := newRepairTransaction(opts.Now())
	for _, item := range paths {
		check := inspectConfig(item.scope, item.path)
		if item.scope == "global" {
			check.SnapshotPath = lastKnownGoodConfigPath()
		}
		report.Checks = append(report.Checks, check)
		if !opts.Apply || !check.Exists || check.Valid || (opts.OnlyScope != "" && item.scope != opts.OnlyScope) || (item.scope == "project" && !opts.IncludeProject) {
			continue
		}
		quarantine := item.path + ".reasonix-quarantine-" + opts.Now().UTC().Format("20060102T150405Z")
		if err := os.Rename(item.path, quarantine); err != nil {
			return report, fmt.Errorf("quarantine %s config: %w", item.scope, err)
		}
		report.Applied = append(report.Applied, "quarantined "+item.scope+" config at "+quarantine)
		tx.Changes = append(tx.Changes, RepairChange{TargetPath: item.path, PreviousPath: quarantine, Scope: item.scope})
		if err := persistRepairTransaction(tx); err != nil {
			_ = os.Rename(quarantine, item.path)
			return report, err
		}
		if item.scope == "global" {
			if err := restoreLastKnownGoodConfig(item.path); err == nil {
				report.Applied = append(report.Applied, "restored global config from last-known-good snapshot")
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

func inspectConfig(scope, path string) ConfigCheck {
	check := ConfigCheck{Scope: scope, Path: path, Valid: true}
	if path == "" {
		return check
	}
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			check.Valid = false
			check.Error = err.Error()
		}
		return check
	}
	check.Exists = true
	if err := config.ValidateFile(path); err != nil {
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
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := config.ValidateFile(path); err != nil {
		return err
	}
	snapshot := lastKnownGoodConfigPath()
	if snapshot == "" {
		return nil
	}
	if err := fileutil.AtomicWriteFile(snapshot, b, 0o600); err != nil {
		return err
	}
	now := time.Now().UTC()
	meta := snapshotMeta{SchemaVersion: 1, SourcePath: path, RecordedAt: now.Format(time.RFC3339Nano), Version: version}
	encoded, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(snapshot+".json", append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	return recordConfigSnapshot(path, b, version, now)
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
	if err := config.ValidateFile(snapshot); err != nil {
		return err
	}
	b, err := os.ReadFile(snapshot)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(dest, b, 0o600)
}
