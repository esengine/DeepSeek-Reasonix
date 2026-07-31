//go:build linux

package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aead.dev/minisign"
)

func TestSanitizeHelperErrorStripsPaths(t *testing.T) {
	err := errors.New("/home/user/cache/foo.deb locked by /var/lib/dpkg/lock")
	got := sanitizeHelperError(err)
	if strings.Contains(got, "/home/") || strings.Contains(got, "/var/") {
		t.Fatalf("path leaked: %q", got)
	}
	if !strings.Contains(got, "<path>") {
		t.Fatalf("expected path redaction, got %q", got)
	}
}

func TestIsPackageManagerBusy(t *testing.T) {
	if !isPackageManagerBusy(errors.New("Could not get lock /var/lib/dpkg/lock-frontend")) {
		t.Fatal("expected busy detection")
	}
	if isPackageManagerBusy(errors.New("dependency problems prevent configuration")) {
		t.Fatal("dependency failure is not busy")
	}
}

func TestAcceptDebIdentity(t *testing.T) {
	cases := []struct {
		name    string
		id      debIdentity
		goArch  string
		wantErr string
	}{
		{
			name:   "ok amd64",
			id:     debIdentity{Package: packageName, Version: "1.2.3", Arch: "amd64"},
			goArch: "amd64",
		},
		{
			name:    "wrong package",
			id:      debIdentity{Package: "evil", Version: "1.2.3", Arch: "amd64"},
			goArch:  "amd64",
			wantErr: "package name rejected",
		},
		{
			name:    "wrong arch",
			id:      debIdentity{Package: packageName, Version: "1.2.3", Arch: "arm64"},
			goArch:  "amd64",
			wantErr: "package architecture rejected",
		},
		{
			name:   "arch all allowed",
			id:     debIdentity{Package: packageName, Version: "1.2.3", Arch: "all"},
			goArch: "amd64",
		},
		{
			name:    "missing version",
			id:      debIdentity{Package: packageName, Version: "", Arch: "amd64"},
			goArch:  "amd64",
			wantErr: "package version missing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := acceptDebIdentity(tc.id, tc.goArch)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestAcceptVersionTransition(t *testing.T) {
	tests := []struct {
		name          string
		cmp           int
		candidate     string
		installed     string
		wantDowngrade bool
		wantErr       bool
	}{
		{name: "ordinary upgrade", cmp: 1, candidate: "1.3.0", installed: "1.2.0"},
		{name: "equal rejected", cmp: 0, candidate: "1.2.0", installed: "1.2.0", wantErr: true},
		{name: "preview to older stable", cmp: -1, candidate: "1.2.0", installed: "1.3.0~preview.1", wantDowngrade: true},
		{name: "stable to older stable rejected", cmp: -1, candidate: "1.2.0", installed: "1.3.0", wantErr: true},
		{name: "preview to older preview rejected", cmp: -1, candidate: "1.2.0~preview.1", installed: "1.3.0~preview.1", wantErr: true},
		{name: "rc to stable rejected", cmp: -1, candidate: "1.2.0", installed: "1.3.0~rc.1", wantErr: true},
		{name: "internal preview rejected", cmp: -1, candidate: "1.2.0", installed: "1.3.0~preview.1+internal", wantErr: true},
		{name: "malformed stable rejected", cmp: -1, candidate: "01.2.0", installed: "1.3.0~preview.1", wantErr: true},
		{name: "malformed preview rejected", cmp: -1, candidate: "1.2.0", installed: "1.3.0~preview.01", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowDowngrade, err := acceptVersionTransition(tt.cmp, tt.candidate, tt.installed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if allowDowngrade != tt.wantDowngrade {
				t.Fatalf("allowDowngrade = %v, want %v", allowDowngrade, tt.wantDowngrade)
			}
		})
	}
}

func TestAptInstallArgvFixed(t *testing.T) {
	argv := aptInstallArgv("/tmp/pkg.deb", false)
	want := []string{
		"/usr/bin/apt-get",
		"install",
		"--assume-yes",
		"--only-upgrade",
		"--no-remove",
		"/tmp/pkg.deb",
	}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v", argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, argv[i], want[i])
		}
	}
	joined := strings.Join(argv, " ")
	for _, bad := range []string{"--allow-unauthenticated", "--allow-downgrades", "--force-yes", "sh -c"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("forbidden apt option present: %s in %v", bad, argv)
		}
	}

	downgradeArgv := aptInstallArgv("/tmp/pkg.deb", true)
	if strings.Count(strings.Join(downgradeArgv, " "), "--allow-downgrades") != 1 {
		t.Fatalf("constrained downgrade argv = %v", downgradeArgv)
	}
	if downgradeArgv[len(downgradeArgv)-1] != "/tmp/pkg.deb" {
		t.Fatalf("package path must remain the final fixed argument: %v", downgradeArgv)
	}
}

func TestParsePhaseLine(t *testing.T) {
	phase, ok := parsePhaseLine(phasePrefix + "installing")
	if !ok || phase != "installing" {
		t.Fatalf("got %q %v", phase, ok)
	}
	if _, ok := parsePhaseLine("noise"); ok {
		t.Fatal("noise should not parse")
	}
}

func TestCopyOwnedRegularFileRejectsSymlinkFailClosed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.deb")
	if err := os.WriteFile(target, []byte("deb"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.deb")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.deb")
	// Any UID — O_NOFOLLOW must fail before ownership is checked.
	if err := copyOwnedRegularFile(link, dst, 0o600, os.Getuid(), maxInputBytes); err == nil {
		t.Fatal("symlink must be rejected without fallback open")
	}
}

func TestCopyOwnedRegularFileRejectsWrongOwner(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; cannot construct a non-self-owned file cheaply")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.deb")
	if err := os.WriteFile(src, []byte("package-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.deb")
	// Claim the file must be owned by root while the test user owns it.
	if err := copyOwnedRegularFile(src, dst, 0o600, 0, maxInputBytes); err == nil {
		t.Fatal("wrong owner must be rejected")
	}
}

func TestCopyOwnedRegularFileRejectsOversize(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.deb")
	if err := os.WriteFile(src, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.deb")
	if err := copyOwnedRegularFile(src, dst, 0o600, os.Getuid(), 4); err == nil {
		t.Fatal("oversize input must be rejected")
	}
}

func TestCopyOwnedRegularFileCopiesMatchingOwner(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.deb")
	payload := []byte("package-bytes")
	if err := os.WriteFile(src, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.deb")
	if err := copyOwnedRegularFile(src, dst, 0o600, os.Getuid(), maxInputBytes); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("copy mismatch: %q", got)
	}
}

// --- runInstall table-driven coverage via injectable deps ---

type installProbe struct {
	phases   []string
	results  []helperResult
	aptCalls []string
}

func baseFakeDeps(p *installProbe) installDeps {
	return installDeps{
		geteuid: func() int { return 0 },
		getenv: func(k string) string {
			if k == "PKEXEC_UID" {
				return "1000"
			}
			return ""
		},
		mkTempDir: func() (string, error) {
			return os.MkdirTemp("", "reasonix-helper-test-*")
		},
		removeAll: os.RemoveAll,
		copyOwnedRegular: func(src, dst string, mode os.FileMode, ownerUID int, maxBytes int64) error {
			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, mode)
		},
		readFile: os.ReadFile,
		verify:   func(data, sig []byte) error { return nil },
		inspectDeb: func(path string) (debIdentity, error) {
			return debIdentity{Package: packageName, Version: "1.2.0", Arch: "amd64"}, nil
		},
		installedVersion: func() (string, error) { return "1.1.0", nil },
		compareVersions:  func(a, b string) (int, error) { return 1, nil },
		aptInstall: func(pkgPath string, allowDowngrade bool) error {
			p.aptCalls = append(p.aptCalls, pkgPath)
			// Assert argv shape through the pure helper used by production.
			argv := aptInstallArgv(pkgPath, allowDowngrade)
			if argv[0] != aptGetPath || argv[1] != "install" {
				return fmt.Errorf("bad argv: %v", argv)
			}
			return nil
		},
		verifyInstalled: func(want string) error {
			if want != "1.2.0" {
				return errors.New("installed version mismatch")
			}
			return nil
		},
		writePhase:    func(phase string) { p.phases = append(p.phases, phase) },
		writeResult:   func(r helperResult) { p.results = append(p.results, r) },
		goArch:        "amd64",
		maxInputBytes: maxInputBytes,
	}
}

func writeInstallInputs(t *testing.T) (pkg, sig string) {
	t.Helper()
	dir := t.TempDir()
	pkg = filepath.Join(dir, "Reasonix.deb")
	sig = filepath.Join(dir, "Reasonix.deb.minisig")
	if err := os.WriteFile(pkg, []byte("deb-payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sig, []byte("sig-payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	return pkg, sig
}

func TestRunInstallSuccessEmitsInstallingPhaseBeforeApt(t *testing.T) {
	pkg, sig := writeInstallInputs(t)
	var probe installProbe
	d := baseFakeDeps(&probe)
	aptStarted := false
	d.aptInstall = func(pkgPath string, allowDowngrade bool) error {
		aptStarted = true
		if allowDowngrade {
			t.Fatal("ordinary upgrade unexpectedly enabled downgrades")
		}
		if len(probe.phases) == 0 || probe.phases[0] != "installing" {
			t.Fatalf("phase installing must be emitted before apt; phases=%v", probe.phases)
		}
		probe.aptCalls = append(probe.aptCalls, pkgPath)
		return nil
	}
	code := runWith(d, []string{"install", "--package", pkg, "--signature", sig})
	if code != exitOK {
		t.Fatalf("exit = %d, results=%+v", code, probe.results)
	}
	if !aptStarted || len(probe.aptCalls) != 1 {
		t.Fatalf("apt not called: %+v", probe.aptCalls)
	}
	if len(probe.results) != 1 || !probe.results[0].OK || probe.results[0].Version != "1.2.0" {
		t.Fatalf("result = %+v", probe.results)
	}
}

func TestRunInstallTable(t *testing.T) {
	pkg, sig := writeInstallInputs(t)

	type tc struct {
		name        string
		mutate      func(*installDeps, *installProbe)
		args        []string
		wantCode    int
		wantCodeStr string
		wantPhase   bool // installing emitted only on success path past validation
		wantOK      bool
	}
	cases := []tc{
		{
			name:        "usage missing flags",
			args:        []string{"install"},
			wantCode:    exitUsage,
			wantCodeStr: "usage",
		},
		{
			name: "not root",
			mutate: func(d *installDeps, _ *installProbe) {
				d.geteuid = func() int { return 1000 }
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitNotRoot,
			wantCodeStr: "not_root",
		},
		{
			name: "missing PKEXEC_UID",
			mutate: func(d *installDeps, _ *installProbe) {
				d.getenv = func(string) string { return "" }
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitNotRoot,
			wantCodeStr: "not_root",
		},
		{
			name: "bad input copy",
			mutate: func(d *installDeps, _ *installProbe) {
				d.copyOwnedRegular = func(string, string, os.FileMode, int, int64) error {
					return errors.New("owner mismatch")
				}
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitBadInput,
			wantCodeStr: "bad_input",
		},
		{
			name: "verify failed",
			mutate: func(d *installDeps, _ *installProbe) {
				d.verify = func([]byte, []byte) error { return errors.New("bad sig") }
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitVerifyFailed,
			wantCodeStr: "verify_failed",
		},
		{
			name: "wrong package name",
			mutate: func(d *installDeps, _ *installProbe) {
				d.inspectDeb = func(string) (debIdentity, error) {
					return debIdentity{Package: "other", Version: "2.0", Arch: "amd64"}, nil
				}
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitPackageRejected,
			wantCodeStr: "package_rejected",
		},
		{
			name: "wrong architecture",
			mutate: func(d *installDeps, _ *installProbe) {
				d.inspectDeb = func(string) (debIdentity, error) {
					return debIdentity{Package: packageName, Version: "2.0", Arch: "arm64"}, nil
				}
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitPackageRejected,
			wantCodeStr: "package_rejected",
		},
		{
			name: "same version rejected",
			mutate: func(d *installDeps, _ *installProbe) {
				d.compareVersions = func(a, b string) (int, error) { return 0, nil }
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitPackageRejected,
			wantCodeStr: "package_rejected",
		},
		{
			name: "downgrade rejected",
			mutate: func(d *installDeps, _ *installProbe) {
				d.compareVersions = func(a, b string) (int, error) { return -1, nil }
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitPackageRejected,
			wantCodeStr: "package_rejected",
		},
		{
			name: "preview to stable downgrade allowed",
			mutate: func(d *installDeps, p *installProbe) {
				d.inspectDeb = func(string) (debIdentity, error) {
					return debIdentity{Package: packageName, Version: "1.2.0", Arch: "amd64"}, nil
				}
				d.installedVersion = func() (string, error) { return "1.3.0~preview.1", nil }
				d.compareVersions = func(a, b string) (int, error) { return -1, nil }
				d.aptInstall = func(pkgPath string, allowDowngrade bool) error {
					if !allowDowngrade {
						return errors.New("preview-to-stable downgrade flag missing")
					}
					p.aptCalls = append(p.aptCalls, pkgPath)
					return nil
				}
			},
			args:      []string{"install", "--package", pkg, "--signature", sig},
			wantCode:  exitOK,
			wantPhase: true,
			wantOK:    true,
		},
		{
			name: "apt busy",
			mutate: func(d *installDeps, _ *installProbe) {
				d.aptInstall = func(string, bool) error {
					return errors.New("Could not get lock /var/lib/dpkg/lock-frontend")
				}
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitBusy,
			wantCodeStr: "package_manager_busy",
			wantPhase:   true,
		},
		{
			name: "apt non-zero",
			mutate: func(d *installDeps, _ *installProbe) {
				d.aptInstall = func(string, bool) error {
					return errors.New("E: Sub-process /usr/bin/dpkg returned an error code (1)")
				}
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitInstallFailed,
			wantCodeStr: "install_failed",
			wantPhase:   true,
		},
		{
			name: "post install version mismatch",
			mutate: func(d *installDeps, _ *installProbe) {
				d.verifyInstalled = func(string) error { return errors.New("installed version mismatch") }
			},
			args:        []string{"install", "--package", pkg, "--signature", sig},
			wantCode:    exitPostVerify,
			wantCodeStr: "package_verify_failed",
			wantPhase:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var probe installProbe
			d := baseFakeDeps(&probe)
			if tc.mutate != nil {
				tc.mutate(&d, &probe)
			}
			code := runWith(d, tc.args)
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d; results=%+v", code, tc.wantCode, probe.results)
			}
			if len(probe.results) == 0 {
				t.Fatal("expected a written result")
			}
			last := probe.results[len(probe.results)-1]
			if last.OK != tc.wantOK {
				t.Fatalf("result OK = %v, want %v: %+v", last.OK, tc.wantOK, last)
			}
			if tc.wantCodeStr != "" && last.Code != tc.wantCodeStr {
				t.Fatalf("code = %q, want %q", last.Code, tc.wantCodeStr)
			}
			if tc.wantPhase {
				if len(probe.phases) == 0 || probe.phases[0] != "installing" {
					t.Fatalf("expected installing phase before failure after validation, got %v", probe.phases)
				}
			} else if len(probe.phases) > 0 {
				// Validation failures must not claim apt started.
				t.Fatalf("unexpected phase before validation failure: %v", probe.phases)
			}
		})
	}
}

func TestRunInstallCleansTempDirOnFailure(t *testing.T) {
	pkg, sig := writeInstallInputs(t)
	var probe installProbe
	d := baseFakeDeps(&probe)
	var created, removed string
	d.mkTempDir = func() (string, error) {
		dir, err := os.MkdirTemp("", "reasonix-helper-test-*")
		created = dir
		return dir, err
	}
	d.removeAll = func(path string) error {
		removed = path
		return os.RemoveAll(path)
	}
	d.verify = func([]byte, []byte) error { return errors.New("fail after tmp") }
	code := runWith(d, []string{"install", "--package", pkg, "--signature", sig})
	if code != exitVerifyFailed {
		t.Fatalf("exit = %d", code)
	}
	if created == "" || removed != created {
		t.Fatalf("temp dir not cleaned: created=%q removed=%q", created, removed)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("temp dir still on disk: %v", err)
	}
}

func TestRunInstallRejectsUnknownCommand(t *testing.T) {
	var probe installProbe
	d := baseFakeDeps(&probe)
	code := runWith(d, []string{"upgrade"})
	if code != exitUsage {
		t.Fatalf("exit = %d", code)
	}
}

// TestMinisignRejectsWrongKeyAndTamper pins that the shared verify path used by
// the helper rejects throwaway-key signatures and tampered payloads. Production
// uses update.Verify with the embedded key; here we exercise the same minisign
// library contract the helper depends on.
func TestMinisignRejectsWrongKeyAndTamper(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("artifact")
	sig := minisign.Sign(priv, data)
	if !minisign.Verify(pub, data, sig) {
		t.Fatal("genuine signature under matching key must verify")
	}
	if minisign.Verify(pub, []byte("tampered"), sig) {
		t.Fatal("tampered payload must not verify")
	}
	otherPub, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if minisign.Verify(otherPub, data, sig) {
		t.Fatal("signature under a different public key must not verify")
	}
	// Garbage signature must fail under the matching public key.
	if minisign.Verify(pub, data, []byte("not-a-sig")) {
		t.Fatal("garbage signature must not verify")
	}
	// Wire the same contract into installDeps.verify so a wrong-key path fails
	// runInstall with exitVerifyFailed.
	var probe installProbe
	d := baseFakeDeps(&probe)
	d.verify = func(payload, signature []byte) error {
		if !minisign.Verify(pub, payload, signature) {
			return errors.New("signature verification failed")
		}
		return nil
	}
	pkg, sigPath := writeInstallInputs(t)
	// Input files contain "deb-payload"/"sig-payload", not a real signature.
	code := runWith(d, []string{"install", "--package", pkg, "--signature", sigPath})
	if code != exitVerifyFailed {
		t.Fatalf("exit = %d, want verify failed; results=%+v", code, probe.results)
	}
}
