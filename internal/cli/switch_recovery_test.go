package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// divergedSessionController builds a controller whose in-memory transcript has
// diverged from what path holds on disk, so its next Snapshot hits a conflict
// and retargets the controller to a recovery branch.
func divergedSessionController(t *testing.T, dir, path string) *control.Controller {
	t.Helper()
	disk := agent.NewSession("sys prompt")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := disk.Save(path); err != nil {
		t.Fatalf("save disk session: %v", err)
	}

	stale := agent.NewSession("sys prompt")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	return control.New(control.Options{
		Executor:    agent.New(nil, nil, stale, agent.Options{}, event.Discard),
		SessionDir:  dir,
		SessionPath: path,
		Label:       "deepseek-flash",
	})
}

// TestModelSwitchCarriesRecoveryPathAfterSnapshotConflict is the TUI /model
// twin of the desktop rebuild fix: when the pre-switch Snapshot retargets the
// controller to a recovery branch, the resume path handed to buildController
// must be that recovery path. A pre-snapshot capture bound the just-recovered
// transcript back to the original file, re-conflicting on every later save.
func TestModelSwitchCarriesRecoveryPathAfterSnapshotConflict(t *testing.T) {
	isolateUserConfig(t)
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "model-switch-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, originalPath)
	m.modelRef = "old/old-model"
	var gotResumePath string
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, resumePath string) (*control.Controller, error) {
		gotResumePath = resumePath
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	m.runModelSubcommand("/model deepseek-flash/deepseek-v4-flash")
	if m.pendingModelSwitch == nil {
		t.Fatal("runModelSubcommand did not queue a model switch")
	}
	m.pendingModelSwitch()

	if gotResumePath == "" || gotResumePath == originalPath || !strings.Contains(filepath.Base(gotResumePath), "-recovery-") {
		t.Fatalf("resume path = %q, want recovery path distinct from %q", gotResumePath, originalPath)
	}
	if got := m.ctrl.SessionPath(); got != gotResumePath {
		t.Fatalf("old controller session path = %q, want recovery path %q", got, gotResumePath)
	}
}

// TestEffortSwitchCarriesRecoveryPathAfterSnapshotConflict covers the same
// contract for the TUI /effort rebuild path.
func TestEffortSwitchCarriesRecoveryPathAfterSnapshotConflict(t *testing.T) {
	isolateUserConfig(t)
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "effort-switch-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, originalPath)
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	var gotResumePath string
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, resumePath string) (*control.Controller, error) {
		gotResumePath = resumePath
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	cmd := m.runEffortCommand("/effort max")
	if cmd == nil {
		t.Fatal("runEffortCommand did not queue a rebuild")
	}
	cmd()

	if gotResumePath == "" || gotResumePath == originalPath || !strings.Contains(filepath.Base(gotResumePath), "-recovery-") {
		t.Fatalf("resume path = %q, want recovery path distinct from %q", gotResumePath, originalPath)
	}
	if got := m.ctrl.SessionPath(); got != gotResumePath {
		t.Fatalf("old controller session path = %q, want recovery path %q", got, gotResumePath)
	}
}

// TestSkillRefreshCarriesRecoveryPathAfterSnapshotConflict covers the TUI skill
// rebuild path, which also snapshots then rebuilds the controller in place.
func TestSkillRefreshCarriesRecoveryPathAfterSnapshotConflict(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "skill-refresh-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, originalPath)
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	var gotResumePath string
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, resumePath string) (*control.Controller, error) {
		gotResumePath = resumePath
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	if !m.scheduleSkillSessionRefresh("skill refresh", "") {
		t.Fatal("scheduleSkillSessionRefresh did not queue a rebuild")
	}
	m.pendingModelSwitch()

	if gotResumePath == "" || gotResumePath == originalPath || !strings.Contains(filepath.Base(gotResumePath), "-recovery-") {
		t.Fatalf("resume path = %q, want recovery path distinct from %q", gotResumePath, originalPath)
	}
	if got := m.ctrl.SessionPath(); got != gotResumePath {
		t.Fatalf("old controller session path = %q, want recovery path %q", got, gotResumePath)
	}
}

func TestWorkModeSwitchCarriesRecoveryPathAndMovesLeaseBeforeRebuild(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "work-mode-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, originalPath)
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.runtimeProfile = "full"
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(originalPath); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}
	var gotResumePath, heldAtBuild string
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, resumePath string) (*control.Controller, error) {
		gotResumePath = resumePath
		heldAtBuild = m.leases.HeldPath()
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	cmd := m.runWorkModeCommand("/work-mode delivery")
	if cmd == nil {
		t.Fatal("work-mode switch did not queue a rebuild")
	}
	cmd()

	if gotResumePath == "" || gotResumePath == originalPath || !strings.Contains(filepath.Base(gotResumePath), "-recovery-") {
		t.Fatalf("resume path = %q, want recovery path distinct from %q", gotResumePath, originalPath)
	}
	if got, want := heldAtBuild, agent.CanonicalSessionPath(gotResumePath); got != want {
		t.Fatalf("lease at build = %q, want recovery path %q", got, want)
	}
}

func TestResumeCommandKeepsLeaseOnRecoveryPathWhenTargetHeld(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "resume-active-conflict.jsonl")
	target := filepath.Join(dir, "resume-target.jsonl")
	saveTestSession(t, target, "target session")

	m := newTestChatTUI()
	m.width = 80
	m.ctrl = divergedSessionController(t, dir, active)
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}
	holdSessionLease(t, target)

	m.runResumeCommand(fmt.Sprintf("/resume %d", resumeIndexForPath(t, dir, target)))

	recoveryPath := m.ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == active || recoveryPath == target || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path after refused resume = %q, want recovery path distinct from active %q and target %q", recoveryPath, active, target)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(recoveryPath); got != want {
		t.Fatalf("lease after refused resume = %q, want recovery path %q", got, want)
	}
}

func TestResumePickerKeepsLeaseOnRecoveryPathWhenTargetHeld(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "resume-picker-active-conflict.jsonl")
	target := filepath.Join(dir, "resume-picker-target.jsonl")
	saveTestSession(t, target, "target session")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, active)
	m.resumePick = &resumePicker{sessions: []agent.SessionInfo{{Path: target}}, sel: 0}
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}
	holdSessionLease(t, target)

	next, _ := m.applyResumePick()
	m = next.(chatTUI)

	recoveryPath := m.ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == active || recoveryPath == target || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path after refused picker resume = %q, want recovery path distinct from active %q and target %q", recoveryPath, active, target)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(recoveryPath); got != want {
		t.Fatalf("lease after refused picker resume = %q, want recovery path %q", got, want)
	}
}

func TestCompactDoneKeepsLeaseOnRecoveryPathAfterSnapshotConflict(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "compact-active-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, active)
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}

	next, _ := m.Update(compactDoneMsg{})
	m = next.(chatTUI)

	recoveryPath := m.ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == active || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path after compact snapshot = %q, want recovery path distinct from active %q", recoveryPath, active)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(recoveryPath); got != want {
		t.Fatalf("lease after compact snapshot = %q, want recovery path %q", got, want)
	}
}

func TestBranchTreeKeepsLeaseOnRecoveryPathAfterSnapshotConflict(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "tree-active-conflict.jsonl")

	m := newTestChatTUI()
	m.width = 80
	m.ctrl = divergedSessionController(t, dir, active)
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}

	m.showBranchTree()

	recoveryPath := m.ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == active || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path after tree snapshot = %q, want recovery path distinct from active %q", recoveryPath, active)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(recoveryPath); got != want {
		t.Fatalf("lease after tree snapshot = %q, want recovery path %q", got, want)
	}
}

func TestShutdownMessageSnapshotsCurrentController(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "shutdown-active-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, active)
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}

	next, cmd := m.Update(tuiShutdownMsg{})
	m = next.(chatTUI)
	if cmd == nil {
		t.Fatal("shutdown message should return tea.Quit")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Fatalf("shutdown command = %T, want tea.QuitMsg", msg)
	}

	recoveryPath := m.ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == active || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path after shutdown snapshot = %q, want recovery path distinct from active %q", recoveryPath, active)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(recoveryPath); got != want {
		t.Fatalf("lease after shutdown snapshot = %q, want recovery path %q", got, want)
	}
}

// TestBranchCompletionKeepsLeaseOnRecoveryPathAfterSnapshotConflict covers the
// /switch tab-completion path: listing branches snapshots the session, which
// can retarget the controller to a recovery branch even though no switch runs.
func TestBranchCompletionKeepsLeaseOnRecoveryPathAfterSnapshotConflict(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "completion-active-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, active)
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}

	if _, _, ok := m.branchArgItems("/switch "); !ok {
		t.Fatal("branchArgItems did not handle /switch completion")
	}

	recoveryPath := m.ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == active || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path after completion snapshot = %q, want recovery path distinct from active %q", recoveryPath, active)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(recoveryPath); got != want {
		t.Fatalf("lease after completion snapshot = %q, want recovery path %q", got, want)
	}
}

// TestModelSwitchFailureKeepsLeaseOnRecoveryPathAfterSnapshotConflict covers
// the rebuild-failure branch: the pre-switch snapshot can retarget the kept
// controller to a recovery branch, and a failed build must not leave the lease
// on the stale original path.
func TestModelSwitchFailureKeepsLeaseOnRecoveryPathAfterSnapshotConflict(t *testing.T) {
	isolateUserConfig(t)
	dir := t.TempDir()
	active := filepath.Join(dir, "model-switch-failure-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, active)
	m.modelRef = "old/old-model"
	m.buildController = func(controllerBuildSpec, []provider.Message, string) (*control.Controller, error) {
		return nil, fmt.Errorf("build failed")
	}
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}

	m.runModelSubcommand("/model deepseek-flash/deepseek-v4-flash")
	if m.pendingModelSwitch == nil {
		t.Fatal("runModelSubcommand did not queue a model switch")
	}
	next, _ := m.Update(m.pendingModelSwitch())
	m = next.(chatTUI)

	recoveryPath := m.ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == active || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path after failed switch = %q, want recovery path distinct from active %q", recoveryPath, active)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(recoveryPath); got != want {
		t.Fatalf("lease after failed switch = %q, want recovery path %q", got, want)
	}
}

// TestModelSwitchMovesLeaseToRecoveryPathBeforeRebuild pins the lease-before-
// bind order: the rebuilt controller resumes prevPath for writing inside
// buildController, so the lease must already guard the retargeted path when
// the build starts, not only after modelSwitchMsg lands.
func TestModelSwitchMovesLeaseToRecoveryPathBeforeRebuild(t *testing.T) {
	isolateUserConfig(t)
	dir := t.TempDir()
	active := filepath.Join(dir, "model-switch-lease-order.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, active)
	m.modelRef = "old/old-model"
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}
	var heldAtBuild string
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, _ string) (*control.Controller, error) {
		heldAtBuild = m.leases.HeldPath()
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	m.runModelSubcommand("/model deepseek-flash/deepseek-v4-flash")
	if m.pendingModelSwitch == nil {
		t.Fatal("runModelSubcommand did not queue a model switch")
	}
	m.pendingModelSwitch()

	assertLeaseHeldRecoveryPathAtBuild(t, &m, active, heldAtBuild)
}

// TestEffortSwitchMovesLeaseToRecoveryPathBeforeRebuild covers the same
// lease-before-bind order for the /effort rebuild path.
func TestEffortSwitchMovesLeaseToRecoveryPathBeforeRebuild(t *testing.T) {
	isolateUserConfig(t)
	dir := t.TempDir()
	active := filepath.Join(dir, "effort-switch-lease-order.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, active)
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}
	var heldAtBuild string
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, _ string) (*control.Controller, error) {
		heldAtBuild = m.leases.HeldPath()
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	cmd := m.runEffortCommand("/effort max")
	if cmd == nil {
		t.Fatal("runEffortCommand did not queue a rebuild")
	}
	cmd()

	assertLeaseHeldRecoveryPathAtBuild(t, &m, active, heldAtBuild)
}

// TestSkillRefreshMovesLeaseToRecoveryPathBeforeRebuild covers the same
// lease-before-bind order for the TUI skill rebuild path.
func TestSkillRefreshMovesLeaseToRecoveryPathBeforeRebuild(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "skill-refresh-lease-order.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, active)
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed active lease: %v", err)
	}
	var heldAtBuild string
	m.buildController = func(_ controllerBuildSpec, _ []provider.Message, _ string) (*control.Controller, error) {
		heldAtBuild = m.leases.HeldPath()
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	if !m.scheduleSkillSessionRefresh("skill refresh", "") {
		t.Fatal("scheduleSkillSessionRefresh did not queue a rebuild")
	}
	m.pendingModelSwitch()

	assertLeaseHeldRecoveryPathAtBuild(t, &m, active, heldAtBuild)
}

// assertLeaseHeldRecoveryPathAtBuild verifies that the snapshot retargeted the
// controller to a recovery branch and that the lease already guarded that
// branch when buildController ran.
func assertLeaseHeldRecoveryPathAtBuild(t *testing.T, m *chatTUI, active, heldAtBuild string) {
	t.Helper()
	recoveryPath := m.ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == active || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("session path after switch snapshot = %q, want recovery path distinct from active %q", recoveryPath, active)
	}
	if want := agent.CanonicalSessionPath(recoveryPath); heldAtBuild != want {
		t.Fatalf("lease when build started = %q, want recovery path %q", heldAtBuild, want)
	}
}

func resumeIndexForPath(t *testing.T, dir, path string) int {
	t.Helper()
	for i, session := range recentSessions(dir) {
		if session.Path == path {
			return i + 1
		}
	}
	t.Fatalf("session %q not found in recent sessions", path)
	return 0
}
