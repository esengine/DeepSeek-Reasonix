package repair

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/installlayout"
)

type supersededUpdateFixture struct {
	home        string
	root        string
	target      string
	active      string
	backup      string
	pendingBody []byte
	transaction *UpdateTransaction
	running     string
}

type supersededAppUpdateFixture struct {
	home        string
	target      string
	backup      string
	marker      string
	pendingBody []byte
	transaction *UpdateTransaction
}

func prepareSupersededAppUpdateFixture(t *testing.T, withBackup, withBackupIdentity bool) supersededAppUpdateFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	target := filepath.Join(root, "Reasonix.app")
	marker := filepath.Join(target, "Contents", "Resources", "release.txt")
	executable := filepath.Join(target, "Contents", "MacOS", "reasonix-desktop")
	for _, path := range []string{marker, executable} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("healthy current app"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	backup := target + ".reasonix-update-backup"
	if withBackup {
		backupMarker := filepath.Join(backup, "Contents", "Resources", "release.txt")
		if err := os.MkdirAll(filepath.Dir(backupMarker), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backupMarker, []byte("preserved previous app"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	originalExecutable := repairExecutable
	repairExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { repairExecutable = originalExecutable })
	tx := &UpdateTransaction{
		SchemaVersion: updateTransactionVersion,
		FromVersion:   "v1.19.5",
		ToVersion:     "v1.19.7",
		Platform:      runtime.GOOS + "/amd64",
		TargetKind:    "app-bundle",
		TargetPath:    target,
		BackupPath:    backup,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if withBackupIdentity {
		var err error
		tx.BackupTreeID, err = repairPlanTreeContentStateID(backup)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := createPendingUpdate(tx); err != nil {
		t.Fatal(err)
	}
	pendingBody, err := os.ReadFile(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}
	return supersededAppUpdateFixture{
		home: home, target: target, backup: backup, marker: marker,
		pendingBody: pendingBody, transaction: tx,
	}
}

func TestArchiveSupersededPendingAppBundleUpdatePreservesLegacyBackup(t *testing.T) {
	fixture := prepareSupersededAppUpdateFixture(t, true, false)
	archived, err := ArchiveSupersededPendingAppBundleUpdate("v1.19.5")
	if err != nil || !archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("pending transaction survived: %v", err)
	}
	if _, err := os.Lstat(fixture.backup); !os.IsNotExist(err) {
		t.Fatalf("public rollback path survived: %v", err)
	}
	backups, err := filepath.Glob(fixture.backup + ".reasonix-retired-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("archived backups=%v err=%v", backups, err)
	}
	if body, err := os.ReadFile(filepath.Join(backups[0], "Contents", "Resources", "release.txt")); err != nil || string(body) != "preserved previous app" {
		t.Fatalf("archived backup body=%q err=%v", body, err)
	}
	entries, err := os.ReadDir(filepath.Join(fixture.home, "repair", "legacy-updates"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("transaction archives=%v err=%v", entries, err)
	}
	if body, err := os.ReadFile(fixture.marker); err != nil || string(body) != "healthy current app" {
		t.Fatalf("running app changed=%q err=%v", body, err)
	}
}

func TestArchiveSupersededPendingAppBundleUpdateHealsMissingBackup(t *testing.T) {
	fixture := prepareSupersededAppUpdateFixture(t, false, false)
	fixture.transaction.BackupTreeID = strings.Repeat("a", 64)
	if err := overwritePendingUpdateForTest(fixture.transaction); err != nil {
		t.Fatal(err)
	}
	archived, err := ArchiveSupersededPendingAppBundleUpdate("v1.19.5")
	if err != nil || !archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("missing-backup transaction survived: %v", err)
	}
}

func TestArchiveSupersededPendingAppBundleUpdateLeavesModernRollbackForHealthCommit(t *testing.T) {
	prepareSupersededAppUpdateFixture(t, true, true)
	archived, err := ArchiveSupersededPendingAppBundleUpdate("v1.19.7")
	if err != nil || archived {
		t.Fatalf("modern transaction archived=%v err=%v", archived, err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("modern pending transaction changed: %v", err)
	}
}

func TestArchiveSupersededPendingAppBundleUpdateLeavesCurrentProcessHandoff(t *testing.T) {
	fixture := prepareSupersededAppUpdateFixture(t, false, false)
	fixture.transaction.HandoffOwnerPID = os.Getpid()
	fixture.transaction.HandoffStagingPath = filepath.Join(filepath.Dir(fixture.target), "handoff-stage")
	fixture.transaction.HandoffAppPath = filepath.Join(fixture.transaction.HandoffStagingPath, "Reasonix.app")
	fixture.transaction.HandoffAppTreeID = strings.Repeat("b", 64)
	fixture.transaction.HandoffStagingTreeID = strings.Repeat("c", 64)
	if err := overwritePendingUpdateForTest(fixture.transaction); err != nil {
		t.Fatal(err)
	}
	archived, err := ArchiveSupersededPendingAppBundleUpdate("v1.19.5")
	if err != nil || archived {
		t.Fatalf("current handoff archived=%v err=%v", archived, err)
	}
	if _, err := readPendingUpdateUnchecked(); err != nil {
		t.Fatalf("current handoff transaction changed: %v", err)
	}
}

func TestArchiveSupersededPendingAppBundleUpdateRejectsUnrelatedOlderVersion(t *testing.T) {
	fixture := prepareSupersededAppUpdateFixture(t, true, false)
	archived, err := ArchiveSupersededPendingAppBundleUpdate("v1.19.4")
	if err == nil || archived {
		t.Fatalf("older running version archived=%v err=%v", archived, err)
	}
	assertPendingTransactionUnchanged(t, fixture.pendingBody)
}

func TestArchiveSupersededPendingAppBundleUpdateRestoresStateOnTargetDrift(t *testing.T) {
	fixture := prepareSupersededAppUpdateFixture(t, true, false)
	originalHook := supersededAppUpdateAfterBackupArchive
	supersededAppUpdateAfterBackupArchive = func(string) {
		if err := os.WriteFile(fixture.marker, []byte("changed concurrently"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { supersededAppUpdateAfterBackupArchive = originalHook })
	archived, err := ArchiveSupersededPendingAppBundleUpdate("v1.19.5")
	if err == nil || archived {
		t.Fatalf("drifted target archived=%v err=%v", archived, err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("pending transaction was not restored: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(fixture.backup, "Contents", "Resources", "release.txt")); err != nil || string(body) != "preserved previous app" {
		t.Fatalf("rollback backup was not restored=%q err=%v", body, err)
	}
}

func TestArchiveSupersededPendingAppBundleUpdatePreservesRecreatedBackupPath(t *testing.T) {
	fixture := prepareSupersededAppUpdateFixture(t, true, false)
	originalHook := supersededAppUpdateAfterBackupArchive
	supersededAppUpdateAfterBackupArchive = func(string) {
		marker := filepath.Join(fixture.backup, "Contents", "Resources", "release.txt")
		if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, []byte("concurrently recreated backup"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { supersededAppUpdateAfterBackupArchive = originalHook })

	archived, err := ArchiveSupersededPendingAppBundleUpdate("v1.19.5")
	if err == nil || archived {
		t.Fatalf("recreated backup archived=%v err=%v", archived, err)
	}
	if _, err := ReadPendingUpdate(); err != nil {
		t.Fatalf("pending transaction was not restored: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(fixture.backup, "Contents", "Resources", "release.txt")); err != nil || string(body) != "concurrently recreated backup" {
		t.Fatalf("recreated public backup changed=%q err=%v", body, err)
	}
	backups, err := filepath.Glob(fixture.backup + ".reasonix-retired-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("preserved archived backups=%v err=%v", backups, err)
	}
	if body, err := os.ReadFile(filepath.Join(backups[0], "Contents", "Resources", "release.txt")); err != nil || string(body) != "preserved previous app" {
		t.Fatalf("archived original backup changed=%q err=%v", body, err)
	}
}

func prepareSupersededUpdateFixture(t *testing.T, pendingVersion, runningVersion string) supersededUpdateFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := t.TempDir()
	originalExecutable := repairExecutable
	t.Cleanup(func() { repairExecutable = originalExecutable })

	target := filepath.Join(root, installlayout.DesktopBinaryName())
	if err := os.WriteFile(target, []byte("old desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	guardName := "reasonix-guard"
	if runtime.GOOS == "windows" {
		guardName += ".exe"
	}
	guard := filepath.Join(root, guardName)
	if err := os.WriteFile(guard, []byte("old guard"), 0o700); err != nil {
		t.Fatal(err)
	}
	repairExecutable = func() (string, error) { return guard, nil }
	tx, err := PrepareFileUpdate("v1.18.0", pendingVersion, target, guard)
	if err != nil {
		t.Fatal(err)
	}
	pendingBody, err := os.ReadFile(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}

	runningVersion = canonicalSemver(runningVersion)
	activeDir := filepath.Join(root, filepath.FromSlash(installlayout.VersionDirRelative(runningVersion)))
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(activeDir, installlayout.DesktopBinaryName())
	if err := os.WriteFile(active, []byte("healthy versioned desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := installlayout.WriteCurrent(root, installlayout.CurrentPointer{
		ActiveVersion: runningVersion,
		ActiveDir:     installlayout.VersionDirRelative(runningVersion),
	}); err != nil {
		t.Fatal(err)
	}
	repairExecutable = func() (string, error) { return active, nil }

	return supersededUpdateFixture{
		home: home, root: root, target: target, active: active,
		backup: tx.BackupPath, pendingBody: pendingBody, transaction: tx,
		running: runningVersion,
	}
}

func TestArchiveSupersededPendingFileUpdateHealsGuardDirectoryMismatch(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	if _, err := ReadPendingUpdate(); err == nil || !strings.Contains(err.Error(), "outside the current Guard installation") {
		t.Fatalf("ReadPendingUpdate error = %v, want the reported Guard-directory mismatch", err)
	}

	archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root)
	if err != nil || !archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	if _, err := os.Lstat(PendingUpdatePath()); !os.IsNotExist(err) {
		t.Fatalf("pending transaction survived: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(fixture.home, "repair", "legacy-updates"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive entries=%v err=%v", entries, err)
	}
	archivedBody, err := os.ReadFile(filepath.Join(fixture.home, "repair", "legacy-updates", entries[0].Name()))
	if err != nil || string(archivedBody) != string(fixture.pendingBody) {
		t.Fatalf("archived transaction changed: err=%v\n got=%q\nwant=%q", err, archivedBody, fixture.pendingBody)
	}
	if body, err := os.ReadFile(fixture.backup); err != nil || string(body) != "old desktop" {
		t.Fatalf("rollback backup=%q err=%v", body, err)
	}
	if len(fixture.transaction.Files) != 2 {
		t.Fatalf("release-unit files=%d, want desktop and Guard", len(fixture.transaction.Files))
	}
	if body, err := os.ReadFile(fixture.transaction.Files[1].BackupPath); err != nil || string(body) != "old guard" {
		t.Fatalf("Guard rollback backup=%q err=%v", body, err)
	}
	if body, err := os.ReadFile(fixture.active); err != nil || string(body) != "healthy versioned desktop" {
		t.Fatalf("active desktop=%q err=%v", body, err)
	}
}

func TestArchiveSupersededPendingFileUpdateAcceptsSameVersionReinstall(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.3", "v1.19.3")
	archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root)
	if err != nil || !archived {
		t.Fatalf("same-version healthy reinstall archived=%v err=%v", archived, err)
	}
}

func TestArchiveSupersededPendingFileUpdateRejectsOlderRunningVersion(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.20.0", "v1.19.3")
	if archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root); err == nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	assertPendingTransactionUnchanged(t, fixture.pendingBody)
}

func TestArchiveSupersededPendingFileUpdateRejectsForeignInstall(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	if archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, t.TempDir()); err == nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	assertPendingTransactionUnchanged(t, fixture.pendingBody)
}

func TestArchiveSupersededPendingFileUpdateRejectsNonActiveExecutable(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	other := filepath.Join(filepath.Dir(fixture.active), "other-desktop")
	if err := os.WriteFile(other, []byte("other"), 0o700); err != nil {
		t.Fatal(err)
	}
	repairExecutable = func() (string, error) { return other, nil }
	if archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root); err == nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	assertPendingTransactionUnchanged(t, fixture.pendingBody)
}

func TestArchiveSupersededPendingFileUpdateRejectsPlatformMismatch(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	changed := *fixture.transaction
	changed.Platform = "foreign/amd64"
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}
	changedBody, err := os.ReadFile(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}
	if archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root); err == nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	assertPendingTransactionUnchanged(t, changedBody)
}

func TestArchiveSupersededPendingFileUpdateRejectsNestedLegacyTarget(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	nestedDir := filepath.Join(fixture.root, "other")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedTarget := filepath.Join(nestedDir, installlayout.DesktopBinaryName())
	changed := *fixture.transaction
	changed.TargetPath = nestedTarget
	changed.Files = append([]UpdateTransactionFile(nil), fixture.transaction.Files...)
	for i := range changed.Files {
		changed.Files[i].TargetPath = nestedTarget
	}
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}
	changedBody, err := os.ReadFile(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}
	if archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root); err == nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	assertPendingTransactionUnchanged(t, changedBody)
}

func TestArchiveSupersededPendingFileUpdateDetectsConcurrentReplacement(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	originalAcquire := acquirePendingUpdateLock
	attempted := make(chan struct{})
	release := make(chan struct{})
	acquirePendingUpdateLock = func() (func(), error) {
		close(attempted)
		<-release
		return func() {}, nil
	}
	t.Cleanup(func() { acquirePendingUpdateLock = originalAcquire })

	type outcome struct {
		archived bool
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root)
		done <- outcome{archived: archived, err: err}
	}()
	<-attempted
	changed := *fixture.transaction
	changed.CreatedAt = time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	if err := overwritePendingUpdateForTest(&changed); err != nil {
		t.Fatal(err)
	}
	close(release)
	result := <-done
	if result.err == nil || result.archived || !strings.Contains(result.err.Error(), "transaction changed while waiting") {
		t.Fatalf("archived=%v err=%v", result.archived, result.err)
	}
	current, err := readPendingUpdateUnchecked()
	if err != nil || UpdateTransactionID(current) != UpdateTransactionID(&changed) {
		t.Fatalf("concurrent transaction was not preserved: current=%+v err=%v", current, err)
	}
}

func TestArchiveSupersededPendingFileUpdateRestoresLateConcurrentRewrite(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	changed := *fixture.transaction
	changed.CreatedAt = time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano)
	originalHook := supersededUpdateBeforeArchive
	supersededUpdateBeforeArchive = func(string) {
		if err := overwritePendingUpdateForTest(&changed); err != nil {
			t.Fatalf("rewrite pending transaction: %v", err)
		}
	}
	t.Cleanup(func() { supersededUpdateBeforeArchive = originalHook })

	archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root)
	if err == nil || archived || !strings.Contains(err.Error(), "transaction changed before archival") {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	current, readErr := readPendingUpdateUnchecked()
	if readErr != nil || UpdateTransactionID(current) != UpdateTransactionID(&changed) {
		t.Fatalf("late replacement was not restored: current=%+v err=%v", current, readErr)
	}
	entries, readDirErr := os.ReadDir(filepath.Join(fixture.home, "repair", "legacy-updates"))
	if readDirErr != nil || len(entries) != 0 {
		t.Fatalf("unexpected archive after restored rewrite: entries=%v err=%v", entries, readDirErr)
	}
}

func TestArchiveSupersededPendingFileUpdateSerializesConcurrentRecovery(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	start := make(chan struct{})
	type outcome struct {
		archived bool
		err      error
	}
	done := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root)
			done <- outcome{archived: archived, err: err}
		}()
	}
	close(start)
	archivedCount := 0
	for range 2 {
		result := <-done
		if result.err != nil {
			t.Fatalf("concurrent recovery error: %v", result.err)
		}
		if result.archived {
			archivedCount++
		}
	}
	if archivedCount != 1 {
		t.Fatalf("archived count=%d, want exactly one", archivedCount)
	}
	entries, err := os.ReadDir(filepath.Join(fixture.home, "repair", "legacy-updates"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive entries=%v err=%v", entries, err)
	}
}

func TestArchiveSupersededPendingFileUpdateRejectsMalformedTransaction(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	body := `{"schemaVersion":1,"toVersion":`
	writePendingUpdateRaw(t, body)
	if archived, err := ArchiveSupersededPendingFileUpdate("v1.20.0", t.TempDir()); err == nil || archived {
		t.Fatalf("archived=%v err=%v", archived, err)
	}
	got, err := os.ReadFile(PendingUpdatePath())
	if err != nil || string(got) != body {
		t.Fatalf("malformed transaction changed: got=%q err=%v", got, err)
	}
}

func assertPendingTransactionUnchanged(t *testing.T, want []byte) {
	t.Helper()
	got, err := os.ReadFile(PendingUpdatePath())
	if err != nil || string(got) != string(want) {
		t.Fatalf("pending transaction changed: err=%v\n got=%q\nwant=%q", err, got, want)
	}
}

func TestSupersededUpdateFixtureUsesCurrentPlatform(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	if !strings.HasPrefix(fixture.transaction.Platform, runtime.GOOS+"/") {
		t.Fatalf("transaction platform=%q, want %s", fixture.transaction.Platform, runtime.GOOS)
	}
}
