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

// ConfigEscapeRepairMarker is recorded when a config was auto-repaired so the
// desktop can surface the outcome once with undo and open-file actions. It is
// consumed (deleted) after display.
type ConfigEscapeRepairMarker struct {
	SchemaVersion int    `json:"schemaVersion"`
	Scope         string `json:"scope"`
	Path          string `json:"path"`
	FixedCount    int    `json:"fixedCount"`
	RepairedAt    string `json:"repairedAt"`
	TransactionID string `json:"transactionId,omitempty"`
}

// ConfigEscapeRepairMarkerPath is the on-disk location of the repair marker.
func ConfigEscapeRepairMarkerPath() string {
	if root := config.MemoryUserDir(); root != "" {
		return filepath.Join(root, "repair", "config-escape-repaired.json")
	}
	return ""
}

func recordConfigEscapeRepairMarker(scope, path string, fixedCount int, txID string, now time.Time) {
	markerPath := ConfigEscapeRepairMarkerPath()
	if markerPath == "" {
		return
	}
	marker := ConfigEscapeRepairMarker{
		SchemaVersion: 1,
		Scope:         scope,
		Path:          path,
		FixedCount:    fixedCount,
		RepairedAt:    now.UTC().Format(time.RFC3339),
		TransactionID: txID,
	}
	b, err := json.Marshal(marker)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(markerPath, b, 0o600)
}

// ConfigEscapeCheck reports Windows-path escaping issues found in one config
// file and whether a repair was applied.
type ConfigEscapeCheck struct {
	Path    string                 `json:"path"`
	Exists  bool                   `json:"exists"`
	Fixes   []config.TOMLEscapeFix `json:"fixes"`
	StateID string                 `json:"stateId,omitempty"` // file state bound at preview time
	Applied bool                   `json:"applied,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// ConfigEscapesReport is the combined scan/apply result for the global and
// (optionally) the active project config.
type ConfigEscapesReport struct {
	Global  ConfigEscapeCheck `json:"global"`
	Project ConfigEscapeCheck `json:"project"`
}

// ConfigEscapesOptions configures Inspect/ApplyConfigEscapes.
type ConfigEscapesOptions struct {
	Root           string
	IncludeProject bool
	// ExpectedStates binds a confirmed apply to the exact file states captured
	// at preview time (repairPlanFileState values). When empty for a scope,
	// that scope's fixes are only previewed, never written — this is the
	// project-config safety rule.
	ExpectedStates map[string]string
	Now            func() time.Time
}

// InspectConfigEscapes scans the global config.toml and the active project's
// reasonix.toml (when requested) for Windows paths written with unescaped
// backslashes. It never writes. Project fixes must be confirmed through
// ApplyConfigEscapes with the bound state IDs.
func InspectConfigEscapes(opts ConfigEscapesOptions) (ConfigEscapesReport, error) {
	report := ConfigEscapesReport{}
	global := config.UserConfigPath()
	project := filepath.Join(opts.Root, "reasonix.toml")
	if opts.Root == "" || opts.Root == "." {
		project = "reasonix.toml"
	}
	var err error
	report.Global, err = inspectConfigEscapesAt(global)
	if err != nil {
		return report, err
	}
	if opts.IncludeProject {
		report.Project, err = inspectConfigEscapesAt(project)
		if err != nil {
			return report, err
		}
	}
	return report, nil
}

func inspectConfigEscapesAt(path string) (ConfigEscapeCheck, error) {
	check := ConfigEscapeCheck{Path: path}
	if path == "" {
		return check, nil
	}
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return check, nil
		}
		return check, err
	}
	check.Exists = true
	b, err := os.ReadFile(path)
	if err != nil {
		check.Error = err.Error()
		return check, nil
	}
	fixes, err := config.ScanTOMLPathEscapes(string(b))
	if err != nil {
		check.Error = err.Error()
		return check, nil
	}
	check.Fixes = fixes
	if len(fixes) > 0 {
		check.StateID = repairPlanFileState(path)
	}
	return check, nil
}

// ApplyConfigEscapes applies high-confidence Windows-path escape repairs:
//
//   - the global config.toml is repaired automatically (high-confidence fixes
//     are deterministic and reversible);
//   - the active project's reasonix.toml is only repaired when the caller
//     confirms it by passing the state IDs captured at preview time.
//
// Every write runs inside a repair transaction with a SHA state binding, an
// undo record, and an immutable config snapshot of the repaired bytes, so the
// change is fully reversible.
func ApplyConfigEscapes(opts ConfigEscapesOptions) (ConfigEscapesReport, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	report, err := InspectConfigEscapes(opts)
	if err != nil {
		return report, err
	}
	unlockTransaction, err := lockRepairTransaction()
	if err != nil {
		return report, err
	}
	defer unlockTransaction()
	if err := reconcilePreparedRepairTransaction(); err != nil {
		return report, fmt.Errorf("repair config escapes: reconcile pending mutation: %w", err)
	}

	var targetPaths []string
	if report.Global.Exists && len(report.Global.Fixes) > 0 {
		targetPaths = append(targetPaths, report.Global.Path)
	}
	if report.Project.Exists && len(report.Project.Fixes) > 0 {
		targetPaths = append(targetPaths, report.Project.Path)
	}
	if len(targetPaths) == 0 {
		return report, nil
	}
	unlock, err := lockRepairMutations(targetPaths...)
	if err != nil {
		return report, err
	}
	defer unlock()

	tx := newRepairTransaction(opts.Now())

	if report.Global.Exists && len(report.Global.Fixes) > 0 {
		applied, err := applyConfigEscapeChange("global", report.Global.Path, report.Global.Fixes, report.Global.StateID, opts.ExpectedStates, tx, opts.Now)
		if err != nil {
			return report, err
		}
		report.Global.Applied = applied
	}
	if report.Project.Exists && len(report.Project.Fixes) > 0 {
		// Project configs are never auto-repaired: confirmation requires the
		// exact state IDs captured at preview time.
		if opts.ExpectedStates == nil || opts.ExpectedStates[report.Project.Path] == "" {
			return report, nil
		}
		applied, err := applyConfigEscapeChange("project", report.Project.Path, report.Project.Fixes, report.Project.StateID, opts.ExpectedStates, tx, opts.Now)
		if err != nil {
			return report, err
		}
		report.Project.Applied = applied
	}
	return report, nil
}

// applyConfigEscapeChange writes one fixed config file under a repair
// transaction: the original bytes are moved to a timestamped backup, the
// repaired body is published atomically, and the change is committed so
// UndoLastRepair can restore the original.
func applyConfigEscapeChange(scope, path string, fixes []config.TOMLEscapeFix, stateID string, expectedStates map[string]string, tx *RepairTransaction, now func() time.Time) (bool, error) {
	expected := stateID
	if expectedStates != nil && expectedStates[path] != "" {
		expected = expectedStates[path]
	}
	if expected != "" {
		if err := verifyRepairPlanFileState(path, map[string]string{path: expected}); err != nil {
			return false, fmt.Errorf("config %s changed since preview: %w", path, err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	fixedBody, err := config.ApplyTOMLPathEscapes(string(b), fixes)
	if err != nil {
		return false, fmt.Errorf("config %s: %w", path, err)
	}
	if fixedBody == string(b) {
		return false, nil
	}

	repairMutationBeforeRename(path)
	if expected != "" {
		if err := verifyRepairPlanFileState(path, map[string]string{path: expected}); err != nil {
			return false, err
		}
	}
	backup := path + ".reasonix-escape-backup-" + now().UTC().Format("20060102T150405Z")
	changeIndex := len(tx.Changes)
	tx.Changes = append(tx.Changes, preparedRepairChangeForPrevious(scope, path, backup))
	if err := persistPreparedRepairTransaction(tx); err != nil {
		return false, fmt.Errorf("prepare %s config escape repair: %w", scope, err)
	}
	repairMutationAfterPrepare(path)
	if err := renameRepairNodeNoReplace(path, backup); err != nil {
		return false, fmt.Errorf("backup %s config: %w", scope, err)
	}
	repairMutationAfterRename(path)
	if expected := expected; expected != "" {
		if err := verifyRepairPlanStateIDFor(backup, path, expected); err != nil {
			if restoreErr := restoreRepairNodeIfAbsent(backup, path); restoreErr != nil {
				return false, fmt.Errorf("config %s changed after confirmation and restore failed: %v: %w", path, restoreErr, err)
			}
			return false, err
		}
	}
	if err := fileutil.AtomicWriteFile(path, []byte(fixedBody), 0o600); err != nil {
		restoreErr := restoreRepairNodeIfAbsent(backup, path)
		if restoreErr != nil {
			return false, fmt.Errorf("write fixed %s config: %v; original retained at %s", scope, err, backup)
		}
		return false, fmt.Errorf("write fixed %s config: %w", scope, err)
	}
	if _, err := os.Lstat(backup); err != nil {
		return false, fmt.Errorf("fixed %s config written but backup missing: %v", scope, err)
	}
	if durable, err := commitPreparedRepairTransaction(tx, changeIndex); err != nil {
		if durable {
			return false, fmt.Errorf("commit %s config escape undo state: cleanup pending journal: %w", scope, err)
		}
		return false, fmt.Errorf("commit %s config escape undo state: %w", scope, err)
	}
	// Publish an immutable recovery point for the repaired bytes so a later
	// invalid state has a healthy snapshot to restore from, and record the
	// outcome marker the desktop banner consumes.
	if scope == "global" {
		_ = recordConfigSnapshot(path, []byte(fixedBody), "", now())
		_ = RecordHealthyConfig("")
	}
	recordConfigEscapeRepairMarker(scope, path, len(fixes), tx.ID, now())
	return true, nil
}
