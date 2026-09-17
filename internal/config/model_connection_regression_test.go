package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/fileutil"
)

func TestAuditRepairVerificationPreservesConcurrentSave(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := UserCredentialsPath()
	if err := os.WriteFile(path, []byte("OLD=one\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldHook := fileutil.CrashPoint
	t.Cleanup(func() { fileutil.CrashPoint = oldHook })
	// Publish before verification and assert verification performs no writes.
	if err := os.WriteFile(path, []byte("OLD=one\nNEW=two\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	fileutil.CrashPoint = func(_, target string) {
		if target == path {
			writes++
		}
	}
	if err := probeCredentialTarget(path); err != nil {
		t.Fatal(err)
	}
	if writes != 0 {
		t.Fatal("verification rewrote the credential file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "NEW=two") {
		t.Fatal("repair verification erased a concurrent credential save")
	}
}

func auditCommitFixture(t *testing.T) (*Config, string) {
	t.Helper()
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := UserConfigPath()
	c := Default()
	c.Providers = []ProviderEntry{{Name: "one", Kind: "openai", BaseURL: "https://one.invalid/v1", Model: "chat", APIKeyEnv: "OLD_KEY"}}
	if err := c.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	unlock := LockUserConfigEdits()
	t.Cleanup(unlock)
	unlockCredentials, err := LockUserCredentialEdits()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unlockCredentials)
	return c, path
}

func TestAuditRecoverCommittedPreferenceReceipt(t *testing.T) {
	c, path := auditCommitFixture(t)
	if err := c.BeginModelCredentialCommitLocked(path, "preference", "digest"); err != nil {
		t.Fatal(err)
	}
	c.Language = "zh"
	if err := c.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if err := c.MarkModelCredentialConfigCommittedLocked(path); err != nil {
		t.Fatal(err)
	}
	if err := RecoverModelCredentialCommitsLocked(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := LookupModelSettingsReceipt("preference"); !ok {
		t.Fatal("committed preference without credential slots has no recovered receipt")
	}
}

func TestAuditRecoverDoesNotInventReceiptAfterExternalEdit(t *testing.T) {
	c, path := auditCommitFixture(t)
	if err := c.BeginModelCredentialCommitLocked(path, "ambiguous", "digest"); err != nil {
		t.Fatal(err)
	}
	slot, err := c.StageModelCredentialLocked("new-secret")
	if err != nil {
		t.Fatal(err)
	}
	c.Providers[0].APIKeyEnv = slot
	if err := c.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	// Crash after publishing config, before the committed journal. An external
	// editor then changes the model but keeps the slot reference.
	external, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	external.Providers[0].Model = "external-model"
	if err := external.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if err := RecoverModelCredentialCommitsLocked(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := LookupModelSettingsReceipt("ambiguous"); ok {
		t.Fatal("recovery certified an externally modified config as this request's committed result")
	}
}

func TestAuditCleanupRetainsEvidenceOnFailedRemoval(t *testing.T) {
	c, path := auditCommitFixture(t)
	if err := c.BeginModelCredentialCommitLocked(path, "cleanup", "digest"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StageModelCredentialLocked("new-secret"); err != nil {
		t.Fatal(err)
	}
	journal := c.modelCredentialCommit.journalPath
	if err := os.Remove(UserCredentialsPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(UserCredentialsPath(), 0700); err != nil {
		t.Fatal(err)
	}
	c.CleanupStagedModelCredentialsLocked(path)
	if _, err := os.Stat(journal); err != nil {
		t.Fatalf("failed cleanup discarded recovery evidence: %v", err)
	}
}

func TestAuditRepairRefusesLinkedHome(t *testing.T) {
	outside := t.TempDir()
	path := filepath.Join(outside, ".env")
	if err := os.WriteFile(path, []byte("KEY=value\n"), 0400); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_HOME", linked)
	report, err := DiagnoseCredentials(CredentialDiagnosticOptions{Repair: true})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if diagnosticStatus(report, "repair") != "failed" {
			t.Fatalf("linked home repair status = %s, want failed", diagnosticStatus(report, "repair"))
		}
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0400 {
		t.Fatalf("repair followed linked home and changed outside permissions; status=%s", diagnosticStatus(report, "repair"))
	}
}

func TestRepairPreservesCredentialContentsAndFileIdentity(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := UserCredentialsPath()
	body := []byte("# unrelated content\nEXISTING_KEY=unchanged\n")
	if err := os.WriteFile(path, body, 0400); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	report, err := DiagnoseCredentials(CredentialDiagnosticOptions{Repair: true})
	if err != nil || (runtime.GOOS != "windows" && diagnosticStatus(report, "repair") != "passed") {
		t.Fatalf("repair: %+v, %v", report, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != string(body) || !os.SameFile(before, after) {
		t.Fatal("repair replaced or changed credential content")
	}
}

func TestReceiptQueryRecoversPublishedPreferenceBeforeMark(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := UserConfigPath()
	c := Default()
	if err := c.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	func() {
		unlock := LockUserConfigEdits()
		defer unlock()
		unlockCredentials, err := LockUserCredentialEdits()
		if err != nil {
			t.Fatal(err)
		}
		defer unlockCredentials()
		if err := c.BeginModelCredentialCommitLocked(path, "published-preference", "digest"); err != nil {
			t.Fatal(err)
		}
		c.Language = "zh"
		if err := c.SaveTo(path); err != nil {
			t.Fatal(err)
		}
		// Stop before Mark/Complete, like a process killed just after rename.
	}()
	if _, ok := RecoverModelSettingsReceipt("published-preference"); !ok {
		t.Fatal("query did not recover proven preference publication")
	}
}

func TestProviderEditPathRespectsProjectOverrideOfBuiltins(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(path, []byte("[[providers]]\nname='deepseek'\nkind='openai'\nmodel='project-chat'\nbase_url='https://project.invalid/v1'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := c.ProviderEditPath(root, "deepseek")
	if err != nil || actual != path {
		t.Fatalf("target = %q, %v", actual, err)
	}
}
