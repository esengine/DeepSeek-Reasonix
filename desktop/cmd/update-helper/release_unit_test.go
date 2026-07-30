package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/desktop/internal/update"
	"reasonix/internal/repair"
)

func TestLoadWindowsStagedReleaseUnitPreflightsAllMembersAndPublishesDesktopLast(t *testing.T) {
	staging := t.TempDir()
	for name, content := range map[string]string{
		"reasonix-desktop.exe":       "desktop-v2",
		"reasonix-guard.exe":         "guard-v2",
		"reasonix-launcher.exe":      "launcher-v2",
		"reasonix-update-helper.exe": "helper-v2",
		"reasonix-cli.exe":           "cli-v2",
	} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	useTestWindowsPayloadManifest(t, staging, "v2")
	installDir := t.TempDir()
	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		TargetKind:    "file",
		TargetPath:    filepath.Join(installDir, "reasonix-desktop.exe"),
		Files: []repair.UpdateTransactionFile{
			{TargetPath: filepath.Join(installDir, "reasonix-desktop.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-guard.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-launcher.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-update-helper.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-cli.exe")},
			{TargetPath: filepath.Join(installDir, "Reasonix.exe")},
		},
	}

	members, err := loadWindowsStagedReleaseUnit(claimed, staging)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(members[len(members)-1].targetPath); !strings.EqualFold(got, "reasonix-desktop.exe") {
		t.Fatalf("last published member = %q, want desktop", got)
	}
	var published []string
	receipts, err := publishLoadedFileUpdateReleaseUnit(claimed, members, func(_ *repair.UpdateTransaction, target string, content []byte, _ os.FileMode) (repair.FileUpdateInstallReceipt, error) {
		published = append(published, filepath.Base(target)+"="+string(content))
		return repair.FileUpdateInstallReceipt{TargetPath: target}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != len(members) {
		t.Fatalf("publish receipts = %d, want %d", len(receipts), len(members))
	}
	if got := strings.Join(published, ","); !strings.Contains(got, "Reasonix.exe=launcher-v2") {
		t.Fatalf("portable alias did not reuse launcher payload: %s", got)
	}
	if !strings.HasPrefix(published[len(published)-1], "reasonix-desktop.exe=") {
		t.Fatalf("publish order = %v", published)
	}
}

func TestLoadWindowsStagedReleaseUnitRejectsIncompletePayloadBeforePublish(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, "reasonix-desktop.exe"), []byte("desktop-v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	useTestWindowsPayloadManifest(t, staging, "v2")
	installDir := t.TempDir()
	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		TargetKind:    "file",
		TargetPath:    filepath.Join(installDir, "reasonix-desktop.exe"),
		Files: []repair.UpdateTransactionFile{
			{TargetPath: filepath.Join(installDir, "reasonix-desktop.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-guard.exe")},
		},
	}
	if _, err := loadWindowsStagedReleaseUnit(claimed, staging); err == nil {
		t.Fatal("incomplete staged release unit was accepted")
	}
}

func TestValidateWindowsClaimedReleaseUnitRequiresExactTargets(t *testing.T) {
	_, complete := completeWindowsStagedReleaseUnitForTest(t, "v2")
	for i, file := range complete.Files {
		t.Run("missing-"+strings.ToLower(filepath.Base(file.TargetPath)), func(t *testing.T) {
			claimed := *complete
			claimed.Files = append([]repair.UpdateTransactionFile(nil), complete.Files[:i]...)
			claimed.Files = append(claimed.Files, complete.Files[i+1:]...)
			if err := validateWindowsClaimedReleaseUnit(&claimed); err == nil ||
				!strings.Contains(err.Error(), "omits") {
				t.Fatalf("missing target error = %v", err)
			}
		})
	}
	t.Run("extra", func(t *testing.T) {
		claimed := *complete
		claimed.Files = append(append([]repair.UpdateTransactionFile(nil), complete.Files...),
			repair.UpdateTransactionFile{TargetPath: filepath.Join(filepath.Dir(complete.TargetPath), "other.exe")})
		if err := validateWindowsClaimedReleaseUnit(&claimed); err == nil ||
			!strings.Contains(err.Error(), "unexpected") {
			t.Fatalf("extra target error = %v", err)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		claimed := *complete
		claimed.Files = append(append([]repair.UpdateTransactionFile(nil), complete.Files...), complete.Files[0])
		if err := validateWindowsClaimedReleaseUnit(&claimed); err == nil ||
			!strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate target error = %v", err)
		}
	})
	t.Run("outside", func(t *testing.T) {
		claimed := *complete
		claimed.Files = append([]repair.UpdateTransactionFile(nil), complete.Files...)
		claimed.Files[1].TargetPath = filepath.Join(t.TempDir(), filepath.Base(claimed.Files[1].TargetPath))
		if err := validateWindowsClaimedReleaseUnit(&claimed); err == nil ||
			!strings.Contains(err.Error(), "outside") {
			t.Fatalf("outside target error = %v", err)
		}
	})
	t.Run("primary", func(t *testing.T) {
		claimed := *complete
		claimed.TargetPath = filepath.Join(filepath.Dir(complete.TargetPath), "reasonix-guard.exe")
		if err := validateWindowsClaimedReleaseUnit(&claimed); err == nil ||
			!strings.Contains(err.Error(), "primary") {
			t.Fatalf("primary target error = %v", err)
		}
	})
}

func TestLoadWindowsStagedReleaseUnitDoesNotCreateMissingPortableAlias(t *testing.T) {
	staging := t.TempDir()
	for _, name := range update.WindowsPayloadFileNames() {
		content := "payload:" + name
		if err := os.WriteFile(filepath.Join(staging, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	useTestWindowsPayloadManifest(t, staging, "v2")
	installDir := t.TempDir()
	claimed := &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     "v2",
		TargetKind:    "file",
		TargetPath:    filepath.Join(installDir, "reasonix-desktop.exe"),
		Files: []repair.UpdateTransactionFile{
			{TargetPath: filepath.Join(installDir, "reasonix-desktop.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-guard.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-launcher.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-update-helper.exe")},
			{TargetPath: filepath.Join(installDir, "reasonix-cli.exe")},
			{TargetPath: filepath.Join(installDir, "Reasonix.exe"), MissingBefore: true},
		},
	}

	members, err := loadWindowsStagedReleaseUnit(claimed, staging)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if strings.EqualFold(filepath.Base(member.targetPath), "Reasonix.exe") {
			t.Fatalf("missing portable alias was added to publish set: %+v", members)
		}
	}
}

func TestPublishLoadedFileUpdateReleaseUnitStopsOnFirstFailedCompareAndPublish(t *testing.T) {
	claimed := &repair.UpdateTransaction{TargetKind: "file"}
	members := []stagedFileUpdateMember{
		{targetPath: "guard.exe", content: []byte("guard"), mode: 0o700},
		{targetPath: "desktop.exe", content: []byte("desktop"), mode: 0o700},
	}
	var published []string
	receipts, err := publishLoadedFileUpdateReleaseUnit(claimed, members, func(_ *repair.UpdateTransaction, target string, _ []byte, _ os.FileMode) (repair.FileUpdateInstallReceipt, error) {
		published = append(published, target)
		return repair.FileUpdateInstallReceipt{}, errors.New("concurrent recreation")
	})
	if err == nil || !strings.Contains(err.Error(), "concurrent recreation") {
		t.Fatalf("publish error = %v", err)
	}
	if len(published) != 1 {
		t.Fatalf("published members = %v, want one attempted member", published)
	}
	if len(receipts) != 0 {
		t.Fatalf("failed first publish returned receipts: %+v", receipts)
	}
}

func TestLoadWindowsStagedReleaseUnitRejectsUnverifiedPayloadBeforePublish(t *testing.T) {
	staging := t.TempDir()
	for _, name := range []string{
		"reasonix-desktop.exe",
		"reasonix-guard.exe",
		"reasonix-launcher.exe",
		"reasonix-update-helper.exe",
		"reasonix-cli.exe",
	} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte(name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeWindowsPayloadManifestForTest(t, staging, "v2")
	acceptWindowsPayloadManifestForTest(t)
	_, claimed := completeWindowsStagedReleaseUnitForTest(t, "v2")
	original := readVerifiedWindowsStagedPayloadFn
	readVerifiedWindowsStagedPayloadFn = func(string) ([]byte, error) {
		return nil, errors.New("payload signature rejected")
	}
	t.Cleanup(func() { readVerifiedWindowsStagedPayloadFn = original })

	if _, err := loadWindowsStagedReleaseUnit(claimed, staging); err == nil {
		t.Fatal("unsigned staged payload must be rejected before publication")
	}
}

func TestLoadWindowsStagedReleaseUnitReadsEachSourceThroughVerifier(t *testing.T) {
	staging := t.TempDir()
	for name, content := range map[string]string{
		"reasonix-desktop.exe":       "desktop-v2",
		"reasonix-guard.exe":         "guard-v2",
		"reasonix-launcher.exe":      "launcher-v2",
		"reasonix-update-helper.exe": "helper-v2",
		"reasonix-cli.exe":           "cli-v2",
	} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeWindowsPayloadManifestForTest(t, staging, "v2")
	acceptWindowsPayloadManifestForTest(t)
	_, claimed := completeWindowsStagedReleaseUnitForTest(t, "v2")
	var verified []string
	original := readVerifiedWindowsStagedPayloadFn
	readVerifiedWindowsStagedPayloadFn = func(path string) ([]byte, error) {
		verified = append(verified, filepath.Base(path))
		return os.ReadFile(path)
	}
	t.Cleanup(func() { readVerifiedWindowsStagedPayloadFn = original })

	if _, err := loadWindowsStagedReleaseUnit(claimed, staging); err != nil {
		t.Fatal(err)
	}
	if len(verified) != len(update.WindowsPayloadFileNames()) {
		t.Fatalf("verified payload count = %d, want %d (%v)", len(verified), len(update.WindowsPayloadFileNames()), verified)
	}
}

func TestLoadWindowsStagedReleaseUnitRejectsMissingManifest(t *testing.T) {
	staging, claimed := completeWindowsStagedReleaseUnitForTest(t, "v2")
	useUnverifiedPayloadReaderForTest(t)
	if _, err := loadWindowsStagedReleaseUnit(claimed, staging); err == nil ||
		!strings.Contains(err.Error(), update.WindowsPayloadManifestName) {
		t.Fatalf("missing signed manifest error = %v", err)
	}
}

func TestLoadWindowsStagedReleaseUnitRejectsBadManifestSignature(t *testing.T) {
	staging, claimed := completeWindowsStagedReleaseUnitForTest(t, "v2")
	writeWindowsPayloadManifestForTest(t, staging, "v2")
	useUnverifiedPayloadReaderForTest(t)
	if _, err := loadWindowsStagedReleaseUnit(claimed, staging); err == nil ||
		!strings.Contains(err.Error(), "verify signed release manifest") {
		t.Fatalf("bad signed manifest error = %v", err)
	}
}

func TestLoadWindowsStagedReleaseUnitRejectsManifestVersionDrift(t *testing.T) {
	staging, claimed := completeWindowsStagedReleaseUnitForTest(t, "v2")
	writeWindowsPayloadManifestForTest(t, staging, "v3")
	acceptWindowsPayloadManifestForTest(t)
	useUnverifiedPayloadReaderForTest(t)
	if _, err := loadWindowsStagedReleaseUnit(claimed, staging); err == nil ||
		!strings.Contains(err.Error(), "identity does not match") {
		t.Fatalf("release version drift error = %v", err)
	}
}

func TestLoadWindowsStagedReleaseUnitRejectsManifestMemberHashDrift(t *testing.T) {
	staging, claimed := completeWindowsStagedReleaseUnitForTest(t, "v2")
	writeWindowsPayloadManifestForTest(t, staging, "v2")
	acceptWindowsPayloadManifestForTest(t)
	useUnverifiedPayloadReaderForTest(t)
	if err := os.WriteFile(filepath.Join(staging, "reasonix-guard.exe"), []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWindowsStagedReleaseUnit(claimed, staging); err == nil ||
		!strings.Contains(err.Error(), "does not match the signed release manifest") {
		t.Fatalf("release member drift error = %v", err)
	}
}

func completeWindowsStagedReleaseUnitForTest(t *testing.T, version string) (string, *repair.UpdateTransaction) {
	t.Helper()
	staging := t.TempDir()
	for _, name := range update.WindowsPayloadFileNames() {
		if err := os.WriteFile(filepath.Join(staging, name), []byte("payload:"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	installDir := t.TempDir()
	files := make([]repair.UpdateTransactionFile, 0, len(update.WindowsPayloadFileNames())+1)
	for _, name := range update.WindowsPayloadFileNames() {
		files = append(files, repair.UpdateTransactionFile{TargetPath: filepath.Join(installDir, name)})
	}
	files = append(files, repair.UpdateTransactionFile{
		TargetPath:    filepath.Join(installDir, "Reasonix.exe"),
		MissingBefore: true,
	})
	return staging, &repair.UpdateTransaction{
		SchemaVersion: 1,
		ToVersion:     version,
		TargetKind:    "file",
		TargetPath:    filepath.Join(installDir, "reasonix-desktop.exe"),
		Files:         files,
	}
}

func writeWindowsPayloadManifestForTest(t *testing.T, staging, version string) {
	t.Helper()
	hashes := make(map[string]string)
	for _, name := range update.WindowsPayloadFileNames() {
		content, err := os.ReadFile(filepath.Join(staging, name))
		if os.IsNotExist(err) {
			content = []byte("missing:" + name)
		} else if err != nil {
			t.Fatal(err)
		}
		hashes[name] = update.WindowsPayloadSHA256(content)
	}
	manifest, err := update.EncodeWindowsPayloadManifest(version, hashes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, update.WindowsPayloadManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, update.WindowsPayloadSignatureName), []byte("test signature"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func acceptWindowsPayloadManifestForTest(t *testing.T) {
	t.Helper()
	original := verifyWindowsPayloadManifestFn
	verifyWindowsPayloadManifestFn = func(_, signature []byte) error {
		if string(signature) != "test signature" {
			return errors.New("unexpected test signature")
		}
		return nil
	}
	t.Cleanup(func() { verifyWindowsPayloadManifestFn = original })
}

func useTestWindowsPayloadManifest(t *testing.T, staging, version string) {
	t.Helper()
	writeWindowsPayloadManifestForTest(t, staging, version)
	acceptWindowsPayloadManifestForTest(t)
	useUnverifiedPayloadReaderForTest(t)
}

func useUnverifiedPayloadReaderForTest(t *testing.T) {
	t.Helper()
	original := readVerifiedWindowsStagedPayloadFn
	readVerifiedWindowsStagedPayloadFn = os.ReadFile
	t.Cleanup(func() { readVerifiedWindowsStagedPayloadFn = original })
}
