package repair

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	textdiff "reasonix/internal/diff"
)

const RepairPlanSchemaVersion = 1

type RepairPlan struct {
	SchemaVersion int                `json:"schemaVersion"`
	Summary       string             `json:"summary"`
	Actions       []RepairPlanAction `json:"actions"`
}

type RepairPlanAction struct {
	Type       string `json:"type"`
	Scope      string `json:"scope,omitempty"`
	SnapshotID string `json:"snapshotId,omitempty"`
	Target     string `json:"target,omitempty"`
	Reason     string `json:"reason"`
}

type RepairPlanPreview struct {
	Index       int    `json:"index"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Diff        string `json:"diff,omitempty"`
	// StateID binds non-display inputs without exposing their paths or content.
	StateID string `json:"stateId,omitempty"`

	fileStates    map[string]string
	afterContent  []byte
	afterReadable bool
}

type repairPlanFileSnapshot struct {
	StateID  string
	Content  []byte
	Readable bool
}

type repairPlanFileStateDescriptor struct {
	Target     string `json:"target"`
	Kind       string `json:"kind"`
	Mode       uint32 `json:"mode,omitempty"`
	LinkTarget string `json:"linkTarget,omitempty"`
	Content    string `json:"content,omitempty"`
}

// RepairPlanID identifies the canonical plan content without trusting an ID
// supplied by a caller. It changes when the summary, action list, or any
// action field changes.
func RepairPlanID(plan RepairPlan) string {
	canonical := struct {
		SchemaVersion int                `json:"schemaVersion"`
		Summary       string             `json:"summary"`
		Actions       []RepairPlanAction `json:"actions"`
	}{plan.SchemaVersion, plan.Summary, plan.Actions}
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// RepairPlanPreviewID binds a plan to the exact preview shown to the user.
// This includes the current filesystem-derived descriptions and diffs, so a
// changed file or action set cannot reuse an earlier confirmation.
func RepairPlanPreviewID(plan RepairPlan, previews []RepairPlanPreview) string {
	canonical := struct {
		PlanID  string              `json:"planId"`
		Preview []RepairPlanPreview `json:"preview"`
	}{RepairPlanID(plan), previews}
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func repairPlanStateID(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func repairPlanActionPreviewID(action RepairPlanAction, preview RepairPlanPreview) string {
	preview.Index = 1
	return repairPlanStateID(struct {
		Action  RepairPlanAction  `json:"action"`
		Preview RepairPlanPreview `json:"preview"`
	}{action, preview})
}

func repairPlanFileSnapshotAt(path string) repairPlanFileSnapshot {
	return repairPlanFileSnapshotFor(path, path)
}

// repairPlanFileSnapshotFor reads the node at readPath but binds identityPath
// into StateID. After a confirmed rename the quarantine path still proves the
// original destination's content without treating the quarantine suffix as a
// different confirmed target.
func repairPlanFileSnapshotFor(readPath, identityPath string) repairPlanFileSnapshot {
	// Target binds the real destination into StateID without exposing the path
	// in the exported preview: confirmation for project A cannot be replayed
	// against project B even when both files have identical content.
	state := repairPlanFileStateDescriptor{Target: repairPlanTargetIdentity(identityPath), Kind: "missing"}
	info, err := os.Lstat(readPath)
	if err != nil {
		if !os.IsNotExist(err) {
			state.Kind = "unreadable"
		}
		return repairPlanFileSnapshot{StateID: repairPlanStateID(state)}
	}
	snapshot := repairPlanFileSnapshot{}
	state.Kind = "other"
	state.Mode = uint32(info.Mode())
	if info.Mode()&os.ModeSymlink != 0 {
		state.Kind = "symlink"
		state.LinkTarget, _ = os.Readlink(readPath)
	} else if info.Mode().IsRegular() {
		state.Kind = "file"
	} else if info.IsDir() {
		state.Kind = "directory"
	}
	if b, readErr := os.ReadFile(readPath); readErr == nil {
		snapshot.Content = b
		snapshot.Readable = true
	}
	snapshot.StateID = repairPlanReadStateIDFor(
		identityPath,
		info.Mode(),
		state.Kind,
		state.LinkTarget,
		snapshot.Content,
		snapshot.Readable,
	)
	return snapshot
}

// repairPlanReadStateIDFor binds the exact bytes or link target consumed by a
// repair operation. Checking the path before and after a read is insufficient:
// an uncooperative writer can temporarily replace its contents during the read
// and restore the expected node before the second path-based check.
func repairPlanReadStateIDFor(
	identityPath string,
	mode os.FileMode,
	kind, linkTarget string,
	content []byte,
	readable bool,
) string {
	state := repairPlanFileStateDescriptor{
		Target:     repairPlanTargetIdentity(identityPath),
		Kind:       kind,
		Mode:       uint32(mode),
		LinkTarget: linkTarget,
	}
	if readable {
		sum := sha256.Sum256(content)
		state.Content = hex.EncodeToString(sum[:])
	} else if state.Kind == "file" || state.Kind == "symlink" {
		state.Kind += "-unreadable"
	}
	return repairPlanStateID(state)
}

// repairPlanPublishedFileMode returns the FileMode that Lstat will report after
// publishing a regular file with the requested permission bits. Windows only
// honors the write bit and surfaces regular files as 0444 or 0666; pre-create
// ownership bindings must use that observed mode or undo will refuse to remove
// a file this repair just created.
func repairPlanPublishedFileMode(perm os.FileMode) os.FileMode {
	perm &= os.ModePerm
	if runtime.GOOS == "windows" {
		if perm&0o222 == 0 {
			return 0o444
		}
		return 0o666
	}
	return perm
}

// repairPlanPreparedCreateStateID binds a remove-on-undo create intent to the
// node that AtomicCreateFile will publish for the given content and mode.
func repairPlanPreparedCreateStateID(identityPath string, content []byte, perm os.FileMode) string {
	return repairPlanReadStateIDFor(
		identityPath,
		repairPlanPublishedFileMode(perm),
		"file",
		"",
		content,
		true,
	)
}

func repairPlanFileState(path string) string {
	return repairPlanFileSnapshotAt(path).StateID
}

func verifyRepairPlanStateIDFor(readPath, identityPath, expected string) error {
	actual := repairPlanFileSnapshotFor(readPath, identityPath).StateID
	if expected != actual {
		return fmt.Errorf("repair plan preview changed since confirmation; re-preview and re-confirm (expected %s, got %s)", expected, actual)
	}
	return nil
}

func repairPlanDerivedStateSnapshot(target string) (string, map[string]string) {
	paths := derivedStatePaths()
	names := []string{target}
	if target == "all" {
		names = make([]string, 0, len(paths))
		for name := range paths {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	states := make([]struct {
		Name  string `json:"name"`
		State string `json:"state"`
	}, 0, len(names))
	fileStates := make(map[string]string, len(names))
	for _, name := range names {
		path := paths[name]
		stateID := repairPlanFileState(path)
		states = append(states, struct {
			Name  string `json:"name"`
			State string `json:"state"`
		}{Name: name, State: stateID})
		fileStates[path] = stateID
	}
	return repairPlanStateID(states), fileStates
}

type ApplyPlanOptions struct {
	Root         string
	AllowProject bool
	// ExpectedPreviewID binds application to the preview that was confirmed.
	// Empty preserves direct package callers that do not model an approval
	// boundary; they are still bound to the preview captured at the start of
	// this ApplyRepairPlan invocation. CLI confirmation paths always populate it.
	ExpectedPreviewID string
}

type ApplyPlanResult struct {
	Applied []string `json:"applied"`
}

func DecodeRepairPlan(data []byte) (RepairPlan, error) {
	data = bytes.TrimSpace(data)
	if bytes.HasPrefix(data, []byte("```")) {
		if start := bytes.IndexByte(data, '{'); start >= 0 {
			if end := bytes.LastIndexByte(data, '}'); end >= start {
				data = data[start : end+1]
			}
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var plan RepairPlan
	if err := dec.Decode(&plan); err != nil {
		return RepairPlan{}, fmt.Errorf("decode repair plan: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return RepairPlan{}, fmt.Errorf("decode repair plan: trailing JSON")
		}
		return RepairPlan{}, fmt.Errorf("decode repair plan: %w", err)
	}
	if err := ValidateRepairPlan(plan); err != nil {
		return RepairPlan{}, err
	}
	return plan, nil
}

func ValidateRepairPlan(plan RepairPlan) error {
	if plan.SchemaVersion != RepairPlanSchemaVersion {
		return fmt.Errorf("repair plan schemaVersion must be %d", RepairPlanSchemaVersion)
	}
	if len(plan.Actions) > 8 {
		return fmt.Errorf("repair plan must contain at most 8 actions")
	}
	if len(plan.Summary) > 1000 {
		return fmt.Errorf("repair plan summary is too long")
	}
	if containsPlanControl(plan.Summary) {
		return fmt.Errorf("repair plan summary contains control characters")
	}
	for i, action := range plan.Actions {
		if len(action.Reason) > 500 {
			return fmt.Errorf("repair action %d reason is too long", i+1)
		}
		if containsPlanControl(action.Reason) {
			return fmt.Errorf("repair action %d reason contains control characters", i+1)
		}
		switch action.Type {
		case "repair_config":
			if action.Scope != "global" && action.Scope != "project" {
				return fmt.Errorf("repair action %d: repair_config scope must be global or project", i+1)
			}
			if action.SnapshotID != "" || action.Target != "" {
				return fmt.Errorf("repair action %d: repair_config has unexpected parameters", i+1)
			}
		case "restore_snapshot":
			if strings.TrimSpace(action.SnapshotID) == "" || action.Scope != "" || action.Target != "" {
				return fmt.Errorf("repair action %d: restore_snapshot requires only snapshotId", i+1)
			}
		case "rebuild_derived_state":
			switch action.Target {
			case "tabs", "projects", "window", "zoom", "all":
			default:
				return fmt.Errorf("repair action %d: invalid derived-state target", i+1)
			}
			if action.Scope != "" || action.SnapshotID != "" {
				return fmt.Errorf("repair action %d: rebuild_derived_state has unexpected parameters", i+1)
			}
		case "rollback_update":
			if action.Scope != "" || action.SnapshotID != "" || action.Target != "" {
				return fmt.Errorf("repair action %d: rollback_update takes no parameters", i+1)
			}
		default:
			return fmt.Errorf("repair action %d: type %q is not allowed", i+1, action.Type)
		}
	}
	return nil
}

func containsPlanControl(text string) bool {
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func PreviewRepairPlan(plan RepairPlan, opts ApplyPlanOptions) ([]RepairPlanPreview, error) {
	if err := ValidateRepairPlan(plan); err != nil {
		return nil, err
	}
	previews := make([]RepairPlanPreview, 0, len(plan.Actions))
	for i, action := range plan.Actions {
		preview := RepairPlanPreview{Index: i + 1, Type: action.Type}
		switch action.Type {
		case "repair_config":
			if action.Scope == "project" && !opts.AllowProject {
				return nil, fmt.Errorf("action %d requires --allow-project", i+1)
			}
			path := config.UserConfigPath()
			if action.Scope == "project" {
				path = projectConfigPath(opts.Root)
			}
			before := repairPlanFileSnapshotAt(path)
			after := repairPlanFileSnapshot{StateID: "none"}
			if action.Scope == "global" {
				after = repairPlanFileSnapshotAt(lastKnownGoodConfigPath())
			}
			preview.Description = "Quarantine invalid " + action.Scope + " configuration"
			preview.Diff = textdiff.Build(action.Scope+"-config.toml", string(before.Content), string(after.Content), textdiff.Modify).Diff
			preview.StateID = repairPlanStateID(struct {
				Before string `json:"before"`
				After  string `json:"after"`
			}{before.StateID, after.StateID})
			preview.fileStates = map[string]string{path: before.StateID}
			if action.Scope == "global" {
				preview.fileStates[lastKnownGoodConfigPath()] = after.StateID
			}
			preview.afterContent = append([]byte(nil), after.Content...)
			preview.afterReadable = after.Readable
		case "restore_snapshot":
			snap, err := configSnapshotByID(action.SnapshotID)
			if err != nil {
				return nil, err
			}
			after := repairPlanFileSnapshotAt(snap.Path)
			metadata := repairPlanFileSnapshotAt(snap.Path + ".json")
			collection := repairPlanFileSnapshotAt(snapshotDir())
			if !after.Readable {
				return nil, fmt.Errorf("config snapshot %q is unreadable", snap.ID)
			}
			if err := verifyConfirmedConfigSnapshot(snap, after.Content); err != nil {
				return nil, err
			}
			before := repairPlanFileSnapshotAt(config.UserConfigPath())
			preview.Description = "Restore verified global configuration snapshot " + snap.ID
			preview.Diff = textdiff.Build("global-config.toml", string(before.Content), string(after.Content), textdiff.Modify).Diff
			preview.StateID = repairPlanStateID(struct {
				Current    string `json:"current"`
				Snapshot   string `json:"snapshot"`
				Metadata   string `json:"metadata"`
				Collection string `json:"collection"`
			}{before.StateID, after.StateID, metadata.StateID, collection.StateID})
			preview.fileStates = map[string]string{
				config.UserConfigPath(): before.StateID,
				snap.Path:               after.StateID,
				snap.Path + ".json":     metadata.StateID,
				snapshotDir():           collection.StateID,
			}
			preview.afterContent = append([]byte(nil), after.Content...)
			preview.afterReadable = after.Readable
		case "rebuild_derived_state":
			preview.Description = "Quarantine and rebuild derived desktop state: " + action.Target
			preview.StateID, preview.fileStates = repairPlanDerivedStateSnapshot(action.Target)
		case "rollback_update":
			tx, err := ReadPendingUpdate()
			if err != nil {
				return nil, fmt.Errorf("action %d: no rollback-ready update: %w", i+1, err)
			}
			preview.Description = fmt.Sprintf("Restore Reasonix %s over probationary %s", tx.FromVersion, tx.ToVersion)
			preview.StateID, preview.fileStates = pendingUpdateBoundPreview(tx)
		}
		previews = append(previews, preview)
	}
	return previews, nil
}

func ApplyRepairPlan(plan RepairPlan, opts ApplyPlanOptions) (ApplyPlanResult, error) {
	preview, err := PreviewRepairPlan(plan, opts)
	if err != nil {
		return ApplyPlanResult{Applied: []string{}}, err
	}
	expected := strings.TrimSpace(opts.ExpectedPreviewID)
	if expected != "" {
		actual := RepairPlanPreviewID(plan, preview)
		if expected != actual {
			return ApplyPlanResult{Applied: []string{}}, fmt.Errorf("repair plan preview changed since confirmation; re-preview and re-confirm (expected %s, got %s)", expected, actual)
		}
	}
	unlockTransaction, err := lockRepairTransaction()
	if err != nil {
		return ApplyPlanResult{Applied: []string{}}, err
	}
	defer unlockTransaction()
	if err := reconcilePreparedRepairTransaction(); err != nil {
		return ApplyPlanResult{}, fmt.Errorf("reconcile pending repair mutation: %w", err)
	}
	boundPreview := preview
	result := ApplyPlanResult{Applied: []string{}}
	// Every mutating action appends to this shared transaction before it persists
	// progress. This keeps the complete applied prefix durable even if the process
	// exits inside an action, before control returns to this loop.
	planTx := newRepairTransaction(time.Now())
	for i, action := range plan.Actions {
		// Empty ExpectedPreviewID only skips the cross-invocation confirmation
		// check above. The filesystem state observed by this invocation's preview
		// is always rechecked under the mutation locks before applying.
		applied, actionErr := applyRepairPlanAction(plan, action, boundPreview[i], opts, planTx, true)
		result.Applied = append(result.Applied, applied...)
		if actionErr != nil {
			return result, fmt.Errorf("action %d: %w", i+1, actionErr)
		}
	}
	return result, nil
}

func applyRepairPlanAction(
	plan RepairPlan,
	action RepairPlanAction,
	bound RepairPlanPreview,
	opts ApplyPlanOptions,
	planTx *RepairTransaction,
	enforcePreview bool,
) ([]string, error) {
	if action.Type == "rollback_update" {
		// Target locks are taken inside rollback under the pending-update
		// lock so Guard and the updater serialize on the same release-unit
		// paths. Re-check the bound preview after those locks are held.
		var rollback UpdateRollbackResult
		var err error
		if enforcePreview {
			rollback, err = rollbackPendingUpdateState(bound.StateID, bound.fileStates)
		} else {
			rollback, err = RollbackPendingUpdate()
		}
		if err != nil {
			return nil, err
		}
		if !rollback.RolledBack {
			if enforcePreview {
				return nil, fmt.Errorf("repair plan preview changed since confirmation; re-preview and re-confirm")
			}
			return nil, nil
		}
		return []string{"rolled back update to " + rollback.ToVersion}, nil
	}

	paths, err := repairPlanActionMutationPaths(action, opts)
	if err != nil {
		return nil, err
	}
	if enforcePreview {
		paths = paths[:0]
		for path := range bound.fileStates {
			paths = append(paths, path)
		}
	}
	unlock, err := lockRepairMutations(paths...)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if enforcePreview {
		current, previewErr := PreviewRepairPlan(RepairPlan{
			SchemaVersion: plan.SchemaVersion,
			Summary:       plan.Summary,
			Actions:       []RepairPlanAction{action},
		}, opts)
		if previewErr != nil {
			return nil, fmt.Errorf("repair plan preview changed since confirmation; re-preview and re-confirm: %w", previewErr)
		}
		expectedAction := repairPlanActionPreviewID(action, bound)
		actualAction := repairPlanActionPreviewID(action, current[0])
		if expectedAction != actualAction {
			return nil, fmt.Errorf("repair plan preview changed since confirmation; re-preview and re-confirm (expected %s, got %s)", expectedAction, actualAction)
		}
	}
	expectedStates := bound.fileStates
	confirmedContent := bound.afterContent
	hasConfirmedContent := bound.afterReadable
	if !enforcePreview {
		expectedStates = nil
		confirmedContent = nil
		hasConfirmedContent = false
	}

	switch action.Type {
	case "repair_config":
		report, err := inspectAndRepairConfigUnlocked(ConfigOptions{
			Root:                   opts.Root,
			Apply:                  true,
			IncludeProject:         action.Scope == "project",
			OnlyScope:              action.Scope,
			expectedStates:         expectedStates,
			confirmedGlobalRestore: confirmedContent,
			hasConfirmedRestore:    hasConfirmedContent,
			repairTransaction:      planTx,
		})
		if err != nil {
			return nil, err
		}
		return report.Applied, nil
	case "restore_snapshot":
		tx, err := restoreConfigSnapshotBoundUnlocked(action.SnapshotID, expectedStates, confirmedContent, planTx)
		if err != nil {
			return nil, err
		}
		return []string{"restored config snapshot (undo " + tx.ID + ")"}, nil
	case "rebuild_derived_state":
		return rebuildDerivedStateBoundUnlocked(action.Target, expectedStates, planTx)
	default:
		return nil, fmt.Errorf("unsupported repair action %q", action.Type)
	}
}

// pendingUpdateBoundPreview binds the pending transaction identity and the
// live release-unit nodes that rollback would displace. The transaction alone
// is not enough: another installer can replace the current binaries while the
// pending JSON stays unchanged. App bundles bind a full tree digest for both
// the live bundle and its backup so interior executable drift invalidates the
// confirmation.
func pendingUpdateBoundPreview(tx *UpdateTransaction) (string, map[string]string) {
	if tx == nil {
		return "", nil
	}
	files := pendingUpdateFiles(tx)
	type unitState struct {
		State string `json:"state"`
	}
	current := make([]unitState, 0, len(files)+2)
	fileStates := make(map[string]string, len(files)+2)
	bind := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := fileStates[path]; ok {
			return
		}
		stateID := repairPlanReleaseNodeState(path)
		fileStates[path] = stateID
		current = append(current, unitState{State: stateID})
	}
	for _, f := range files {
		bind(f.TargetPath)
		if strings.TrimSpace(f.BackupPath) != "" {
			bind(f.BackupPath)
		}
	}
	if tx.TargetKind == "file" {
		bind(installedFileUpdateStatePath(tx))
	}
	bind(tx.TargetPath)
	if strings.EqualFold(strings.TrimSpace(tx.TargetKind), "app-bundle") || strings.TrimSpace(tx.BackupPath) != "" {
		bind(tx.BackupPath)
	}
	return repairPlanStateID(struct {
		Transaction string      `json:"transaction"`
		Current     []unitState `json:"current"`
	}{repairPlanStateID(tx), current}), fileStates
}

// repairPlanReleaseNodeState binds either a single-file identity or a full
// directory tree digest. Directory kind/mode alone is not enough for .app
// bundles: interior executables can change without touching the root node.
func repairPlanReleaseNodeState(path string) string {
	return repairPlanReleaseNodeStateFor(path, path)
}

func repairPlanReleaseNodeStateFor(readPath, identityPath string) string {
	info, err := os.Lstat(readPath)
	if err != nil {
		return repairPlanFileSnapshotFor(readPath, identityPath).StateID
	}
	if info.IsDir() {
		return repairPlanTreeStateIDFor(readPath, identityPath)
	}
	return repairPlanFileSnapshotFor(readPath, identityPath).StateID
}

func verifyRepairPlanReleaseNodeStateFor(readPath, identityPath, expected string) error {
	actual := repairPlanReleaseNodeStateFor(readPath, identityPath)
	if expected != actual {
		return fmt.Errorf("repair plan preview changed since confirmation; re-preview and re-confirm (expected %s, got %s)", expected, actual)
	}
	return nil
}

type repairPlanTreeEntry struct {
	Rel     string `json:"rel"`
	Kind    string `json:"kind"`
	Mode    uint32 `json:"mode,omitempty"`
	Content string `json:"content,omitempty"`
}

func repairPlanTreeEntries(root string) ([]repairPlanTreeEntry, error) {
	entries := make([]repairPlanTreeEntry, 0, 64)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			entries = append(entries, repairPlanTreeEntry{Rel: path, Kind: "unreadable"})
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if rel == "." {
			rel = ""
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			entries = append(entries, repairPlanTreeEntry{Rel: rel, Kind: "unreadable"})
			return nil
		}
		entry := repairPlanTreeEntry{Rel: filepath.ToSlash(rel), Mode: uint32(info.Mode())}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entry.Kind = "symlink"
			if target, readErr := os.Readlink(path); readErr == nil {
				entry.Content = target
			} else {
				entry.Kind = "symlink-unreadable"
			}
		case info.IsDir():
			entry.Kind = "directory"
		case info.Mode().IsRegular():
			entry.Kind = "file"
			if sum, hashErr := hashFile(path); hashErr == nil {
				entry.Content = sum
			} else {
				entry.Kind = "file-unreadable"
			}
		default:
			entry.Kind = "other"
		}
		entries = append(entries, entry)
		return nil
	})
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Rel == entries[j].Rel {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Rel < entries[j].Rel
	})
	return entries, walkErr
}

// repairPlanTreeContentStateID hashes a directory tree without binding its
// root path. This lets an update handoff prove that the staged bundle and the
// installed bundle contain the same bytes even though they live at different
// paths.
func repairPlanTreeContentStateID(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("expected directory, got %s", info.Mode().Type())
	}
	entries, err := repairPlanTreeEntries(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		switch entry.Kind {
		case "unreadable", "file-unreadable", "symlink-unreadable":
			return "", fmt.Errorf("cannot read bundle entry %q", entry.Rel)
		case "other":
			return "", fmt.Errorf("unsupported bundle entry %q", entry.Rel)
		}
	}
	return repairPlanStateID(entries), nil
}

func repairPlanTreeStateIDFor(readRoot, identityRoot string) string {
	entries, err := repairPlanTreeEntries(readRoot)
	if err != nil {
		entries = []repairPlanTreeEntry{{Rel: readRoot, Kind: "unreadable"}}
	}
	return repairPlanStateID(struct {
		Target  string                `json:"target"`
		Entries []repairPlanTreeEntry `json:"entries"`
	}{repairPlanTargetIdentity(identityRoot), entries})
}

func pendingUpdateFiles(tx *UpdateTransaction) []UpdateTransactionFile {
	if tx == nil {
		return nil
	}
	if len(tx.Files) > 0 {
		return tx.Files
	}
	return []UpdateTransactionFile{{
		TargetPath: tx.TargetPath,
		BackupPath: tx.BackupPath,
		SHA256:     tx.BackupSHA256,
	}}
}

func pendingUpdateTargetPaths(tx *UpdateTransaction) []string {
	if tx == nil {
		return nil
	}
	files := pendingUpdateFiles(tx)
	paths := make([]string, 0, len(files)+1)
	seen := map[string]struct{}{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, f := range files {
		add(f.TargetPath)
	}
	add(tx.TargetPath)
	if strings.EqualFold(strings.TrimSpace(tx.TargetKind), "app-bundle") {
		add(tx.BackupPath)
	}
	return paths
}

func repairPlanActionMutationPaths(action RepairPlanAction, opts ApplyPlanOptions) ([]string, error) {
	switch action.Type {
	case "repair_config":
		return configRepairTargetPaths(ConfigOptions{Root: opts.Root, IncludeProject: action.Scope == "project", OnlyScope: action.Scope})
	case "restore_snapshot":
		dir, contentPath, metadataPath, err := configSnapshotPaths(action.SnapshotID)
		if err != nil {
			return nil, err
		}
		return []string{config.UserConfigPath(), dir, contentPath, metadataPath}, nil
	case "rebuild_derived_state":
		return derivedStateTargetPaths(action.Target)
	default:
		return nil, nil
	}
}

func verifyRepairPlanFileState(path string, expectedStates map[string]string) error {
	if len(expectedStates) == 0 {
		return nil
	}
	expected, ok := expectedStates[path]
	if !ok {
		return fmt.Errorf("repair plan preview did not bind target state; re-preview and re-confirm")
	}
	return verifyRepairPlanStateID(path, expected)
}

func verifyRepairPlanFileStates(expectedStates map[string]string) error {
	if len(expectedStates) == 0 {
		return nil
	}
	paths := make([]string, 0, len(expectedStates))
	for path := range expectedStates {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := verifyRepairPlanFileState(path, expectedStates); err != nil {
			return err
		}
	}
	return nil
}

func verifyRepairPlanStateID(path, expected string) error {
	actual := repairPlanFileState(path)
	if expected != actual {
		return fmt.Errorf("repair plan preview changed since confirmation; re-preview and re-confirm (expected %s, got %s)", expected, actual)
	}
	return nil
}

func configSnapshotByID(id string) (ConfigSnapshot, error) {
	snapshots, err := ListConfigSnapshots()
	if err != nil {
		return ConfigSnapshot{}, err
	}
	for _, snap := range snapshots {
		if snap.ID == id {
			return snap, nil
		}
	}
	return ConfigSnapshot{}, fmt.Errorf("config snapshot %q not found", id)
}

func projectConfigPath(root string) string {
	root = strings.TrimSpace(root)
	if root == "" || root == "." {
		return "reasonix.toml"
	}
	return filepath.Join(root, "reasonix.toml")
}
