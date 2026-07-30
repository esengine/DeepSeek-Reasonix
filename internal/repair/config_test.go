package repair

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestInspectInvalidProjectConfigIsReadOnlyByDefault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := InspectAndRepairConfig(ConfigOptions{Root: root, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) != 2 || report.Checks[1].Valid {
		t.Fatalf("checks = %+v", report.Checks)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("project config was modified without IncludeProject: %v", err)
	}
}

func TestInspectCanQuarantineInvalidProjectConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := InspectAndRepairConfig(ConfigOptions{Root: root, Apply: true, IncludeProject: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks[1].Exists || !report.Checks[1].Valid {
		t.Fatalf("project check after repair = %+v", report.Checks[1])
	}
	if matches, _ := filepath.Glob(path + ".reasonix-quarantine-*"); len(matches) != 1 {
		t.Fatalf("quarantine matches = %v", matches)
	}
}

func TestInspectCanQuarantineDanglingProjectConfigSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI")
	}
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "reasonix.toml")
	if err := os.Symlink(filepath.Join(root, "missing.toml"), path); err != nil {
		t.Fatal(err)
	}
	report, err := InspectAndRepairConfig(ConfigOptions{
		Root:           root,
		Apply:          true,
		IncludeProject: true,
		OnlyScope:      "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Checks[1].Exists || !report.Checks[1].Valid {
		t.Fatalf("project check after dangling-link repair = %+v", report.Checks[1])
	}
	matches, err := filepath.Glob(path + ".reasonix-quarantine-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("dangling symlink quarantine = %v, %v", matches, err)
	}
	info, err := os.Lstat(matches[0])
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("quarantine did not preserve dangling symlink node: %v, %v", info, err)
	}
}

func TestRebuildDerivedStateQuarantinesDanglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI")
	}
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "desktop-tabs.json")
	if err := os.Symlink(filepath.Join(home, "missing-tabs.json"), path); err != nil {
		t.Fatal(err)
	}
	applied, err := RebuildDerivedState("tabs")
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied = %v, want dangling state quarantine", applied)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("dangling derived-state link survived rebuild: %v", err)
	}
	info, err := os.Lstat(applied[0])
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("derived-state quarantine did not preserve symlink: %v, %v", info, err)
	}
}

func TestRebuildDerivedStateDirectRejectsWriteBeforeRename(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	tabs := filepath.Join(home, "desktop-tabs.json")
	if err := os.WriteFile(tabs, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalHook := repairMutationBeforeRename
	var writeErr error
	repairMutationBeforeRename = func(path string) {
		if path == tabs {
			writeErr = os.WriteFile(path, []byte("concurrent"), 0o600)
		}
	}
	t.Cleanup(func() { repairMutationBeforeRename = originalHook })

	if _, err := RebuildDerivedState("tabs"); err == nil ||
		!strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("direct rebuild after drift = %v", err)
	}
	if writeErr != nil {
		t.Fatalf("inject direct rebuild drift: %v", writeErr)
	}
	if got, err := os.ReadFile(tabs); err != nil || string(got) != "concurrent" {
		t.Fatalf("concurrent derived state = %q, %v", got, err)
	}
}

func TestInspectAndRepairConfigDirectRejectsWriteBeforeRename(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(project, []byte("invalid = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalHook := repairMutationBeforeRename
	var writeErr error
	repairMutationBeforeRename = func(path string) {
		if path == project {
			writeErr = os.WriteFile(path, []byte("different = [\n"), 0o600)
		}
	}
	t.Cleanup(func() { repairMutationBeforeRename = originalHook })

	if _, err := InspectAndRepairConfig(ConfigOptions{
		Root:           root,
		Apply:          true,
		IncludeProject: true,
		OnlyScope:      "project",
	}); err == nil || !strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("direct config repair after drift = %v", err)
	}
	if writeErr != nil {
		t.Fatalf("inject direct config drift: %v", writeErr)
	}
	if got, err := os.ReadFile(project); err != nil || string(got) != "different = [\n" {
		t.Fatalf("concurrent project config = %q, %v", got, err)
	}
}

func TestInspectAndRepairConfigDirectRejectsDriftWhileWaitingForTransactionLock(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(project, []byte("invalid = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	transactionKey := repairMutationTestKey(repairTransactionPath())
	originalHook := repairMutationBeforeLock
	var writeErr error
	changed := false
	repairMutationBeforeLock = func(paths []string) {
		if changed || len(paths) != 1 || paths[0] != transactionKey {
			return
		}
		changed = true
		writeErr = os.WriteFile(project, []byte("different = [\n"), 0o600)
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalHook })

	if _, err := InspectAndRepairConfig(ConfigOptions{
		Root:           root,
		Apply:          true,
		IncludeProject: true,
		OnlyScope:      "project",
	}); err == nil || !strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("direct config repair after lock-wait drift = %v", err)
	}
	if writeErr != nil {
		t.Fatalf("inject direct config drift: %v", writeErr)
	}
	if got, err := os.ReadFile(project); err != nil || string(got) != "different = [\n" {
		t.Fatalf("drifted project config = %q, %v", got, err)
	}
}

func TestRebuildDerivedStateDirectRejectsDriftWhileWaitingForTransactionLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	tabs := filepath.Join(home, "desktop-tabs.json")
	if err := os.WriteFile(tabs, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	transactionKey := repairMutationTestKey(repairTransactionPath())
	originalHook := repairMutationBeforeLock
	var writeErr error
	changed := false
	repairMutationBeforeLock = func(paths []string) {
		if changed || len(paths) != 1 || paths[0] != transactionKey {
			return
		}
		changed = true
		writeErr = os.WriteFile(tabs, []byte("changed-while-waiting"), 0o600)
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalHook })

	if _, err := RebuildDerivedState("tabs"); err == nil ||
		!strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("direct derived-state rebuild after lock-wait drift = %v", err)
	}
	if writeErr != nil {
		t.Fatalf("inject direct derived-state drift: %v", writeErr)
	}
	if got, err := os.ReadFile(tabs); err != nil || string(got) != "changed-while-waiting" {
		t.Fatalf("drifted derived state = %q, %v", got, err)
	}
}

func TestRepairRestoresLastKnownGoodGlobalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	original := []byte("default_model = \"deepseek-flash\"\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := InspectAndRepairConfig(ConfigOptions{Root: t.TempDir(), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored config = %q, want %q", got, original)
	}
	if len(report.Applied) != 2 {
		t.Fatalf("applied = %v", report.Applied)
	}
}

func TestConfigSnapshotsRotateAndVerifyHash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	for i := 0; i < configSnapshotRetention+2; i++ {
		content := []byte("default_model = \"model-" + string(rune('a'+i)) + "\"\n")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := RecordHealthyConfig("v1"); err != nil {
			t.Fatal(err)
		}
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != configSnapshotRetention {
		t.Fatalf("snapshots = %d, want %d", len(snapshots), configSnapshotRetention)
	}
	if err := os.WriteFile(snapshots[0].Path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("tampered snapshot was restored")
	}
}

func TestRecordConfigSnapshotDoesNotReplaceExistingID(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	content := []byte("default_model = \"confirmed\"\n")
	now := time.Date(2026, time.July, 29, 1, 2, 3, 4, time.UTC)
	sum := sha256.Sum256(content)
	id := now.Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(sum[:])[:12]
	path := filepath.Join(snapshotDir(), id+".toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	intruder := []byte("default_model = \"existing\"\n")
	if err := os.WriteFile(path, intruder, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recordConfigSnapshot(config.UserConfigPath(), content, "v1", now); err == nil {
		t.Fatal("snapshot publication replaced or accepted a conflicting existing ID")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(intruder) {
		t.Fatalf("existing snapshot = %q, %v; want preserved %q", got, err, intruder)
	}
}

func TestRecordHealthyConfigRejectsTamperedDuplicateSnapshot(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := config.UserConfigPath()
	healthy := []byte("default_model = \"healthy\"\n")
	if err := os.WriteFile(path, healthy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, %v", snapshots, err)
	}
	tampered := []byte("default_model = \"tampered\"\n")
	if err := os.WriteFile(snapshots[0].Path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RecordHealthyConfig("v1"); err == nil {
		t.Fatal("duplicate snapshot deduplication hid tampered content")
	}
	if got, err := os.ReadFile(snapshots[0].Path); err != nil || string(got) != string(tampered) {
		t.Fatalf("tampered snapshot = %q, %v; want preserved", got, err)
	}
}

func TestRecordHealthyConfigReadsSourceAfterMutationLock(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := config.UserConfigPath()
	before := []byte("default_model = \"before-lock\"\n")
	after := []byte("default_model = \"after-lock\"\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockRepairMutations(path)
	if err != nil {
		t.Fatal(err)
	}

	attempted := make(chan struct{}, 1)
	originalHook := repairMutationBeforeLock
	repairMutationBeforeLock = func(keys []string) {
		for _, key := range keys {
			if key == canonicalRepairPath(path) {
				select {
				case attempted <- struct{}{}:
				default:
				}
			}
		}
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalHook })
	done := make(chan error, 1)
	go func() { done <- RecordHealthyConfig("v1") }()
	<-attempted
	select {
	case err := <-done:
		unlock()
		t.Fatalf("RecordHealthyConfig bypassed the held source lock: %v", err)
	default:
	}
	if err := os.WriteFile(path, after, 0o600); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(lastKnownGoodConfigPath()); err != nil || string(got) != string(after) {
		t.Fatalf("last-known-good = %q, %v; want post-lock bytes %q", got, err, after)
	}
}

func TestPruneConfigSnapshotPreservesContentChangedAfterMetadataMove(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	snapshots := recordConfigSnapshotSeries(t, configSnapshotRetention)
	oldest := snapshots[len(snapshots)-1]
	tampered := []byte("default_model = \"concurrent\"\n")
	originalHook := configSnapshotPruneAfterMove
	configSnapshotPruneAfterMove = func(kind, original, _ string) {
		if kind == "metadata" && original == oldest.Path+".json" {
			if err := os.WriteFile(oldest.Path, tampered, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { configSnapshotPruneAfterMove = originalHook })

	next := []byte("default_model = \"next\"\n")
	err := recordConfigSnapshot(
		config.UserConfigPath(),
		next,
		"v1",
		time.Date(2026, time.July, 29, 0, 0, configSnapshotRetention, 0, time.UTC),
	)
	if err == nil || !strings.Contains(err.Error(), "content changed during prune") {
		t.Fatalf("prune error = %v, want content-drift rejection", err)
	}
	if got, readErr := os.ReadFile(oldest.Path); readErr != nil || string(got) != string(tampered) {
		t.Fatalf("concurrent snapshot content = %q, %v; want preserved", got, readErr)
	}
	if _, statErr := os.Stat(oldest.Path + ".json"); statErr != nil {
		t.Fatalf("snapshot metadata was not restored: %v", statErr)
	}
}

func TestPruneConfigSnapshotPreservesRecreatedMetadata(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	snapshots := recordConfigSnapshotSeries(t, configSnapshotRetention)
	oldest := snapshots[len(snapshots)-1]
	metaPath := oldest.Path + ".json"
	metadata, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	originalHook := configSnapshotPruneAfterMove
	configSnapshotPruneAfterMove = func(kind, original, _ string) {
		if kind == "metadata" && original == metaPath {
			if err := os.WriteFile(metaPath, metadata, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { configSnapshotPruneAfterMove = originalHook })

	next := []byte("default_model = \"next\"\n")
	err = recordConfigSnapshot(
		config.UserConfigPath(),
		next,
		"v1",
		time.Date(2026, time.July, 29, 0, 0, configSnapshotRetention, 0, time.UTC),
	)
	if err == nil || !strings.Contains(err.Error(), "metadata was recreated during prune") {
		t.Fatalf("prune error = %v, want metadata-recreation rejection", err)
	}
	if got, readErr := os.ReadFile(metaPath); readErr != nil || string(got) != string(metadata) {
		t.Fatalf("recreated metadata = %q, %v; want preserved", got, readErr)
	}
	if _, statErr := os.Stat(oldest.Path); statErr != nil {
		t.Fatalf("snapshot content was removed after metadata recreation: %v", statErr)
	}
	cleanups, globErr := filepath.Glob(metaPath + ".reasonix-cleanup-*")
	if globErr != nil || len(cleanups) != 1 {
		t.Fatalf("displaced metadata cleanup = %v, %v", cleanups, globErr)
	}
}

func recordConfigSnapshotSeries(t *testing.T, count int) []ConfigSnapshot {
	t.Helper()
	for i := 0; i < count; i++ {
		content := []byte("default_model = \"model-" + string(rune('a'+i)) + "\"\n")
		now := time.Date(2026, time.July, 29, 0, 0, i, 0, time.UTC)
		if err := recordConfigSnapshot(config.UserConfigPath(), content, "v1", now); err != nil {
			t.Fatal(err)
		}
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != count {
		t.Fatalf("snapshots = %d, want %d", len(snapshots), count)
	}
	return snapshots
}

func TestRestoreConfigSnapshotUsesTheBytesItVerified(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	confirmed := []byte("default_model = \"confirmed\"\n")
	if err := os.WriteFile(dest, confirmed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, %v", snapshots, err)
	}
	if err := os.WriteFile(dest, []byte("default_model = \"current\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalRename := snapshotRename
	mutated := false
	snapshotRename = func(oldpath, newpath string) error {
		if oldpath == dest && !mutated {
			mutated = true
			if err := os.WriteFile(snapshots[0].Path, []byte("default_model = \"changed-after-verify\"\n"), 0o600); err != nil {
				return err
			}
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { snapshotRename = originalRename })

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(confirmed) {
		t.Fatalf("restored config = %q (%v), want verified bytes %q", got, err, confirmed)
	}
}

func TestRestoreConfigSnapshotDirectRejectsTargetDriftBeforeRename(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dest := config.UserConfigPath()
	if err := os.WriteFile(dest, []byte("default_model = \"snapshot\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, %v", snapshots, err)
	}
	if err := os.WriteFile(dest, []byte("default_model = \"current\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalHook := repairMutationBeforeRename
	var writeErr error
	repairMutationBeforeRename = func(path string) {
		if path == dest {
			writeErr = os.WriteFile(path, []byte("default_model = \"concurrent\"\n"), 0o600)
		}
	}
	t.Cleanup(func() { repairMutationBeforeRename = originalHook })

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil ||
		!strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("direct snapshot restore after drift = %v", err)
	}
	if writeErr != nil {
		t.Fatalf("inject direct snapshot drift: %v", writeErr)
	}
	if got, err := os.ReadFile(dest); err != nil ||
		string(got) != "default_model = \"concurrent\"\n" {
		t.Fatalf("concurrent config = %q, %v", got, err)
	}
}

func TestRestoreConfigSnapshotDirectRejectsDriftWhileWaitingForTransactionLock(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dest := config.UserConfigPath()
	if err := os.WriteFile(dest, []byte("default_model = \"snapshot\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, %v", snapshots, err)
	}
	if err := os.WriteFile(dest, []byte("default_model = \"current\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	transactionKey := repairMutationTestKey(repairTransactionPath())
	originalHook := repairMutationBeforeLock
	var writeErr error
	changed := false
	repairMutationBeforeLock = func(paths []string) {
		if changed || len(paths) != 1 || paths[0] != transactionKey {
			return
		}
		changed = true
		writeErr = os.WriteFile(dest, []byte("default_model = \"changed-while-waiting\"\n"), 0o600)
	}
	t.Cleanup(func() { repairMutationBeforeLock = originalHook })

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil ||
		!strings.Contains(err.Error(), "preview changed since confirmation") {
		t.Fatalf("direct snapshot restore after lock-wait drift = %v", err)
	}
	if writeErr != nil {
		t.Fatalf("inject direct snapshot drift: %v", writeErr)
	}
	if got, err := os.ReadFile(dest); err != nil ||
		string(got) != "default_model = \"changed-while-waiting\"\n" {
		t.Fatalf("drifted config = %q, %v", got, err)
	}
}

func TestRestoreConfigSnapshotPreservesConcurrentRecreate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	original := []byte("default_model = \"snapshot\"\n")
	if err := os.WriteFile(dest, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, %v", snapshots, err)
	}
	current := []byte("default_model = \"current\"\n")
	if err := os.WriteFile(dest, current, 0o600); err != nil {
		t.Fatal(err)
	}

	concurrent := []byte("default_model = \"concurrent\"\n")
	originalHook := repairMutationAfterRename
	repairMutationAfterRename = func(path string) {
		if path == dest {
			if err := os.WriteFile(dest, concurrent, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { repairMutationAfterRename = originalHook })

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("snapshot restore accepted a target recreated after displacement")
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != string(concurrent) {
		t.Fatalf("concurrent config was overwritten: %q, %v", got, err)
	}
	last, err := ReadLastRepair()
	if err != nil || len(last.Changes) != 1 {
		t.Fatalf("displaced original was not recorded for undo: %+v, %v", last, err)
	}
	if got, err := os.ReadFile(last.Changes[0].PreviousPath); err != nil || string(got) != string(current) {
		t.Fatalf("displaced config = %q, %v; want %q", got, err, current)
	}
}

func TestRestoreConfigSnapshotCrossDeviceFallbackPreservesConcurrentRecreate(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dest := config.UserConfigPath()
	if err := os.WriteFile(dest, []byte("default_model = \"snapshot\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, %v", snapshots, err)
	}
	current := []byte("default_model = \"current\"\n")
	if err := os.WriteFile(dest, current, 0o600); err != nil {
		t.Fatal(err)
	}

	originalRename := snapshotRename
	snapshotRename = func(oldpath, newpath string) error {
		if filepath.Dir(oldpath) != filepath.Dir(newpath) {
			return errors.New("injected cross-device rename")
		}
		return renameRepairNodeNoReplace(oldpath, newpath)
	}
	t.Cleanup(func() { snapshotRename = originalRename })
	concurrent := []byte("default_model = \"concurrent\"\n")
	originalHook := repairMutationAfterRename
	repairMutationAfterRename = func(path string) {
		if path == dest {
			if err := os.WriteFile(dest, concurrent, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { repairMutationAfterRename = originalHook })

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("cross-device fallback accepted a target recreated after displacement")
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != string(concurrent) {
		t.Fatalf("concurrent config was overwritten: %q, %v", got, err)
	}
	last, err := ReadLastRepair()
	if err != nil || len(last.Changes) != 1 {
		t.Fatalf("last repair = %+v, %v", last, err)
	}
	if filepath.Dir(last.Changes[0].PreviousPath) != filepath.Dir(dest) {
		t.Fatalf("fallback backup = %q, want sibling of %q", last.Changes[0].PreviousPath, dest)
	}
	if got, err := os.ReadFile(last.Changes[0].PreviousPath); err != nil || string(got) != string(current) {
		t.Fatalf("fallback backup = %q, %v", got, err)
	}
}

func TestRestoreConfigSnapshotDoesNotPublishWhenUndoPersistenceFails(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dest := config.UserConfigPath()
	snapshot := []byte("default_model = \"snapshot\"\n")
	if err := os.WriteFile(dest, snapshot, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, %v", snapshots, err)
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repairTransactionPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("restore succeeded without durable undo intent")
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatalf("config was published without durable undo intent: %v", err)
	}
}

func TestUndoRepairRestoresQuarantinedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	bad := []byte("[broken\n")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectAndRepairConfig(ConfigOptions{Root: t.TempDir(), Apply: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bad) {
		t.Fatalf("undone config = %q", got)
	}
}

func TestUndoRepairPreservesConcurrentRecreate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := config.UserConfigPath()
	original := []byte("[broken\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectAndRepairConfig(ConfigOptions{Root: t.TempDir(), Apply: true}); err != nil {
		t.Fatal(err)
	}
	repaired := []byte("default_model = \"repaired\"\n")
	if err := os.WriteFile(path, repaired, 0o600); err != nil {
		t.Fatal(err)
	}

	concurrent := []byte("default_model = \"concurrent\"\n")
	originalHook := repairMutationAfterRename
	repairMutationAfterRename = func(target string) {
		if target == path {
			if err := os.WriteFile(path, concurrent, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { repairMutationAfterRename = originalHook })

	if _, err := UndoLastRepair(); err == nil {
		t.Fatal("undo accepted a target recreated after retaining redo")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(concurrent) {
		t.Fatalf("concurrent config was overwritten: %q, %v", got, err)
	}
	redos, err := filepath.Glob(path + ".reasonix-redo-*")
	if err != nil || len(redos) != 1 {
		t.Fatalf("redo candidates = %v, %v", redos, err)
	}
	if got, err := os.ReadFile(redos[0]); err != nil || string(got) != string(repaired) {
		t.Fatalf("retained repaired config = %q, %v; want %q", got, err, repaired)
	}
	last, err := ReadLastRepair()
	if err != nil || last.Undone || last.Changes[0].Undone {
		t.Fatalf("failed undo transaction = %+v, %v", last, err)
	}
	if got, err := os.ReadFile(last.Changes[0].PreviousPath); err != nil || string(got) != string(original) {
		t.Fatalf("undo source = %q, %v; want %q", got, err, original)
	}
}

func TestUndoRepairRejectsQuarantineDrift(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := config.UserConfigPath()
	original := []byte("[broken\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectAndRepairConfig(ConfigOptions{Root: t.TempDir(), Apply: true}); err != nil {
		t.Fatal(err)
	}
	last, err := ReadLastRepair()
	if err != nil || len(last.Changes) != 1 || last.Changes[0].PreviousStateID == "" {
		t.Fatalf("last repair lacks previous-state identity: %+v, %v", last, err)
	}
	repaired := []byte("default_model = \"repaired\"\n")
	if err := os.WriteFile(path, repaired, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(last.Changes[0].PreviousPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := UndoLastRepair(); err == nil || !strings.Contains(err.Error(), "previous state changed") {
		t.Fatalf("undo error = %v, want quarantine-drift rejection", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(repaired) {
		t.Fatalf("undo changed repaired config after quarantine drift: %q, %v", got, err)
	}
	if got, err := os.ReadFile(last.Changes[0].PreviousPath); err != nil || string(got) != "tampered" {
		t.Fatalf("undo changed drifted quarantine: %q, %v", got, err)
	}
}

func TestConfigRepairCommitsWhenAuditLogFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := config.UserConfigPath()
	bad := []byte("[broken\n")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repairLogPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := InspectAndRepairConfig(ConfigOptions{Root: t.TempDir(), Apply: true}); err != nil {
		t.Fatalf("repair must commit despite a failing audit log: %v", err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatalf("undo after audit-log failure: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(bad) {
		t.Fatalf("undone config = %q (%v), want %q", got, err, bad)
	}
}

func TestUndoRejectsTamperedRepairTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	previous := filepath.Join(home, "unrelated.previous")
	if err := os.WriteFile(previous, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx := newRepairTransaction(time.Now())
	tx.Changes = append(tx.Changes, RepairChange{Scope: "global", TargetPath: filepath.Join(t.TempDir(), "arbitrary.txt"), PreviousPath: previous})
	if err := persistRepairTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err == nil {
		t.Fatal("tampered repair transaction was accepted")
	}
}

func TestSnapshotUndoAcrossSeparateStateHome(t *testing.T) {
	home := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", stateHome)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("default_model = \"before\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}
	current := []byte("default_model = \"current\"\n")
	if err := os.WriteFile(path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(current) {
		t.Fatalf("undo restored %q, want %q", got, current)
	}
}

// TestRestoreConfigSnapshotPreservesSymlinkThroughUndo pins the dotfiles
// contract: restoring a snapshot over a symlinked config materializes the
// snapshot as a plain file (without writing through the link), and undo
// brings back the original symlink node itself.
func TestRestoreConfigSnapshotPreservesSymlinkThroughUndo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	dotfiles := filepath.Join(t.TempDir(), "dotfiles-config.toml")
	if err := os.WriteFile(dotfiles, []byte("default_model = \"good\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, dest); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}
	if err := os.WriteFile(dotfiles, []byte("default_model = \"drifted\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("restore should materialize the snapshot as a plain file")
	}
	if got, _ := os.ReadFile(dest); string(got) != "default_model = \"good\"\n" {
		t.Fatalf("restored config = %q", got)
	}
	if got, _ := os.ReadFile(dotfiles); string(got) != "default_model = \"drifted\"\n" {
		t.Fatalf("restore wrote through the symlink: %q", got)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("undo materialized a regular file, want symlink (mode %v)", info.Mode())
	}
	if got, err := os.Readlink(dest); err != nil || got != dotfiles {
		t.Fatalf("restored link target = %q (%v), want %q", got, err, dotfiles)
	}
	if got, _ := os.ReadFile(dest); string(got) != "default_model = \"drifted\"\n" {
		t.Fatalf("config through restored link = %q", got)
	}
}

// TestRestoreConfigSnapshotCrossDeviceCleanupKeepsPlainConfig pins the
// fail-safe contract when the state dir sits on another filesystem: the
// fallback atomically moves the original node to a sibling backup, and a
// transaction-save failure restores it without overwriting another writer.
func TestRestoreConfigSnapshotCrossDeviceCleanupKeepsPlainConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	original := []byte("default_model = \"original\"\n")
	if err := os.WriteFile(dest, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}

	originalRename := snapshotRename
	snapshotRename = func(oldpath, newpath string) error {
		if filepath.Dir(oldpath) != filepath.Dir(newpath) {
			return errors.New("injected cross-device rename")
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { snapshotRename = originalRename })
	// Make saveRepairTransaction fail: its atomic write cannot replace a
	// directory squatting on the transaction path.
	if err := os.MkdirAll(repairTransactionPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("restore with failing transaction save should error")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("config.toml must survive the failed restore: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("config after cleanup = %q, want %q", got, original)
	}
}

// TestRestoreConfigSnapshotCrossDeviceCleanupRestoresSymlink is the symlink
// variant: the sibling backup keeps the original link node, and failure
// cleanup must put that link back without writing through it.
func TestRestoreConfigSnapshotCrossDeviceCleanupRestoresSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	dotfiles := filepath.Join(t.TempDir(), "dotfiles-config.toml")
	if err := os.WriteFile(dotfiles, []byte("default_model = \"linked\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, dest); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}

	originalRename := snapshotRename
	snapshotRename = func(oldpath, newpath string) error {
		if filepath.Dir(oldpath) != filepath.Dir(newpath) {
			return errors.New("injected cross-device rename")
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { snapshotRename = originalRename })
	if err := os.MkdirAll(repairTransactionPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err == nil {
		t.Fatal("restore with failing transaction save should error")
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("config.toml must survive the failed restore: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("cleanup materialized a regular file, want symlink (mode %v)", info.Mode())
	}
	if got, err := os.Readlink(dest); err != nil || got != dotfiles {
		t.Fatalf("restored link target = %q (%v), want %q", got, err, dotfiles)
	}
	if got, _ := os.ReadFile(dotfiles); string(got) != "default_model = \"linked\"\n" {
		t.Fatalf("dotfiles content = %q, must be untouched", got)
	}
}

// TestRestoreConfigSnapshotCommitsWhenAuditLogFails pins the commit boundary:
// last-repair.json is the durable undo state; the append-only audit log is
// best-effort. A failing audit append must not roll the restore back — that
// would consume the backup the just-persisted transaction points to, wedging
// every later UndoLastRepair — and undo itself must still succeed while the
// log stays unwritable.
func TestRestoreConfigSnapshotCommitsWhenAuditLogFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dest := config.UserConfigPath()
	original := []byte("default_model = \"original\"\n")
	if err := os.WriteFile(dest, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordHealthyConfig("v1"); err != nil {
		t.Fatal(err)
	}
	snapshots, err := ListConfigSnapshots()
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %+v, err = %v", snapshots, err)
	}
	current := []byte("default_model = \"current\"\n")
	if err := os.WriteFile(dest, current, 0o600); err != nil {
		t.Fatal(err)
	}
	// Wedge the append-only audit log: a directory squatting on its path makes
	// appendRepairLog fail while last-repair.json still persists fine.
	if err := os.MkdirAll(repairLogPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreConfigSnapshot(snapshots[0].ID); err != nil {
		t.Fatalf("restore must commit despite a failing audit log: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(original) {
		t.Fatalf("restored config = %q, want %q", got, original)
	}
	if _, err := UndoLastRepair(); err != nil {
		t.Fatalf("undo after audit-log failure: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(current) {
		t.Fatalf("undone config = %q, want %q", got, current)
	}
}
