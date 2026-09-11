package repair

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func requireDistinctPOSIXMode(t *testing.T, a, b string) {
	t.Helper()
	modeA, err := os.Lstat(a)
	if err != nil {
		t.Fatal(err)
	}
	modeB, err := os.Lstat(b)
	if err != nil {
		t.Fatal(err)
	}
	if modeA.Mode() == modeB.Mode() {
		t.Skip("filesystem does not preserve POSIX mode bits")
	}
}

func TestRepairPlanTreeContentStateIDIncludesPOSIXMode(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	for _, root := range []string{a, b} {
		if err := os.Mkdir(filepath.Join(root, "Contents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "Contents", "marker"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(b, "Contents", "marker"), 0o700); err != nil {
		t.Fatal(err)
	}
	requireDistinctPOSIXMode(t, filepath.Join(a, "Contents", "marker"), filepath.Join(b, "Contents", "marker"))
	idA, err := repairPlanTreeContentStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := repairPlanTreeContentStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatal("strict digest ignored a POSIX mode change")
	}
}

func TestRepairPlanTreePayloadStateIDIgnoresPOSIXMode(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	for _, root := range []string{a, b} {
		if err := os.Mkdir(filepath.Join(root, "Contents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "Contents", "marker"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(b, "Contents", "marker"), 0o700); err != nil {
		t.Fatal(err)
	}
	requireDistinctPOSIXMode(t, filepath.Join(a, "Contents", "marker"), filepath.Join(b, "Contents", "marker"))
	idA, err := repairPlanTreePayloadStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := repairPlanTreePayloadStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA != idB {
		t.Fatal("payload digest changed for a POSIX mode-only copy")
	}
}

func TestRepairPlanTreePayloadStateIDIgnoresAppleDoubleSidecarFile(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	for _, root := range []string{a, b} {
		if err := os.Mkdir(filepath.Join(root, "Contents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "Contents", "Info.plist"), []byte("plist"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(b, "Contents", "._Info.plist"), []byte("appledouble"), 0o644); err != nil {
		t.Fatal(err)
	}
	idA, err := repairPlanTreePayloadStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := repairPlanTreePayloadStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA != idB {
		t.Fatal("payload digest changed for an AppleDouble sidecar file")
	}
	strictA, err := repairPlanTreeContentStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	strictB, err := repairPlanTreeContentStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if strictA == strictB {
		t.Fatal("strict digest ignored an AppleDouble sidecar file")
	}
}

func TestRepairPlanTreePayloadStateIDHashesAppleDoubleDirectory(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	for _, root := range []string{a, b} {
		if err := os.Mkdir(filepath.Join(root, "Contents"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(b, "._Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	idA, err := repairPlanTreePayloadStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := repairPlanTreePayloadStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatal("payload digest ignored a ._ directory")
	}
}

func TestRepairPlanTreePayloadStateIDHashesAppleDoubleSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires elevated privileges on Windows CI")
	}
	a := t.TempDir()
	b := t.TempDir()
	for _, root := range []string{a, b} {
		if err := os.WriteFile(filepath.Join(root, "marker"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("marker", filepath.Join(b, "._marker")); err != nil {
		t.Fatal(err)
	}
	idA, err := repairPlanTreePayloadStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := repairPlanTreePayloadStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatal("payload digest ignored a ._ symlink")
	}
}

func TestRepairPlanTreePayloadStateIDDetectsPayloadChange(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "marker"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "marker"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	idA, err := repairPlanTreePayloadStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := repairPlanTreePayloadStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatal("payload digest ignored a content change")
	}
}

func TestRepairPlanTreePayloadStateIDDetectsExtraFile(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "marker"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "marker"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "extra"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	idA, err := repairPlanTreePayloadStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := repairPlanTreePayloadStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatal("payload digest ignored an extra file")
	}
}

func TestRepairPlanTreePayloadStateIDHashesOrphanAppleDoubleName(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	if err := os.WriteFile(filepath.Join(a, "marker"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "marker"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "._orphan"), []byte("sidecar"), 0o644); err != nil {
		t.Fatal(err)
	}
	idA, err := repairPlanTreePayloadStateID(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, err := repairPlanTreePayloadStateID(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatal("payload digest ignored an orphan ._ file")
	}
}

func TestRepairPlanTreeHandoffAppMatchesLegacyStrictDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	strict, err := repairPlanTreeContentStateID(root)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := repairPlanTreePayloadStateID(root)
	if err != nil {
		t.Fatal(err)
	}
	if strict == payload {
		t.Fatal("payload digest collapsed onto the strict digest")
	}
	matched, err := repairPlanTreeHandoffAppMatches(root, strict)
	if err != nil || !matched {
		t.Fatalf("legacy strict digest rejected: matched=%v err=%v", matched, err)
	}
	matched, err = repairPlanTreeHandoffAppMatches(root, payload)
	if err != nil || !matched {
		t.Fatalf("payload digest rejected: matched=%v err=%v", matched, err)
	}
}

func TestVerifyAppBundleUpdateHandoffReplacementAcceptsModeOnlyCopy(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	for _, root := range []string{src, dst} {
		if err := os.WriteFile(filepath.Join(root, "marker"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(dst, "marker"), 0o700); err != nil {
		t.Fatal(err)
	}
	requireDistinctPOSIXMode(t, filepath.Join(src, "marker"), filepath.Join(dst, "marker"))
	payload, err := repairPlanTreePayloadStateID(src)
	if err != nil {
		t.Fatal(err)
	}
	tx := &UpdateTransaction{TargetKind: "app-bundle", HandoffAppTreeID: payload}
	if err := VerifyAppBundleUpdateHandoffReplacement(tx, dst); err != nil {
		t.Fatalf("mode-only copy rejected: %v", err)
	}
}

func TestVerifyAppBundleUpdateHandoffReplacementAcceptsLegacyStrictDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "marker"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	strict, err := repairPlanTreeContentStateID(root)
	if err != nil {
		t.Fatal(err)
	}
	tx := &UpdateTransaction{TargetKind: "app-bundle", HandoffAppTreeID: strict}
	if err := VerifyAppBundleUpdateHandoffReplacement(tx, root); err != nil {
		t.Fatalf("legacy strict digest rejected: %v", err)
	}
}

func TestPrepareAppBundleUpdateHandoffRecordsPayloadAppDigest(t *testing.T) {
	tx, _ := prepareTestAppBundleHandoff(t)
	payload, err := repairPlanTreePayloadStateID(tx.HandoffAppPath)
	if err != nil {
		t.Fatal(err)
	}
	strict, err := repairPlanTreeContentStateID(tx.HandoffAppPath)
	if err != nil {
		t.Fatal(err)
	}
	if tx.HandoffAppTreeID != payload {
		t.Fatalf("staged app digest = %s, want payload %s", tx.HandoffAppTreeID, payload)
	}
	if tx.HandoffAppTreeID == strict {
		t.Fatal("staged app digest used the strict tree identity")
	}
	backup, err := repairPlanTreeContentStateID(tx.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if tx.BackupTreeID != backup {
		t.Fatalf("backup digest = %s, want strict %s", tx.BackupTreeID, backup)
	}
	staging, err := repairPlanTreeContentStateID(tx.HandoffStagingPath)
	if err != nil {
		t.Fatal(err)
	}
	if tx.HandoffStagingTreeID != staging {
		t.Fatalf("staging digest = %s, want strict %s", tx.HandoffStagingTreeID, staging)
	}
}
