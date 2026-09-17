package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func diagnosticStatus(report CredentialDiagnosticReport, id string) string {
	for _, check := range report.Checks {
		if check.ID == id {
			return check.Status
		}
	}
	return ""
}

func TestCredentialDiagnosticsRejectsDirectoryAndSymlink(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{name: "directory", setup: func(t *testing.T, path string) {
			t.Helper()
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", setup: func(t *testing.T, path string) {
			t.Helper()
			target := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(target, []byte("KEY=value\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REASONIX_HOME", t.TempDir())
			path := UserCredentialsPath()
			tc.setup(t, path)
			report, err := DiagnoseCredentials(CredentialDiagnosticOptions{Repair: true})
			if err != nil {
				t.Fatal(err)
			}
			if diagnosticStatus(report, "file_type") != "failed" || diagnosticStatus(report, "repair") != "failed" {
				t.Fatalf("report = %+v", report)
			}
		})
	}
}

func TestCredentialDiagnosticsProbeAndDryRunDoNotChangeCredential(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := UserCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("KEEP=secret\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	report, err := DiagnoseCredentials(CredentialDiagnosticOptions{Probe: true, Repair: true, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "KEEP=secret\n" || before.Mode().Perm() != after.Mode().Perm() {
		t.Fatalf("dry-run mutated credentials: mode %o -> %o, body %q", before.Mode().Perm(), after.Mode().Perm(), raw)
	}
	if diagnosticStatus(report, "directory_replace_probe") != "passed" {
		t.Fatalf("report = %+v", report)
	}
	// GitHub's elevated Windows runner creates temporary files owned by the
	// built-in Administrators group, so repair correctly stays fail-closed even
	// though the non-mutating directory probe remains valid there.
	if runtime.GOOS != "windows" && (diagnosticStatus(report, "repair") != "passed" || len(report.Actions) == 0) {
		t.Fatalf("report = %+v", report)
	}
}

func TestCredentialDiagnosticsReportsPendingTransaction(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := UserConfigPath()
	cfg := Default()
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	unlockConfig := LockUserConfigEdits()
	unlockCredentials, err := LockUserCredentialEdits()
	if err != nil {
		unlockConfig()
		t.Fatal(err)
	}
	if err := cfg.BeginModelCredentialCommitLocked(path, "pending", "digest"); err != nil {
		unlockCredentials()
		unlockConfig()
		t.Fatal(err)
	}
	unlockCredentials()
	unlockConfig()
	report, err := DiagnoseCredentials(CredentialDiagnosticOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if report.PendingTransactions != 1 || diagnosticStatus(report, "transactions") != "failed" {
		t.Fatalf("report = %+v", report)
	}
}
