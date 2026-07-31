//go:build linux

// Command reasonix-update-helper (Linux) installs a verified .deb under Polkit.
// It is invoked only via pkexec with fixed argv and re-validates every input as
// root before calling apt-get. The unprivileged desktop process never runs apt.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"reasonix/desktop/internal/update"
)

// Stable exit codes observed by the desktop installer.
const (
	exitOK              = 0
	exitUsage           = 2
	exitNotRoot         = 10
	exitBadInput        = 11
	exitVerifyFailed    = 12
	exitPackageRejected = 13
	exitBusy            = 14
	exitInstallFailed   = 15
	exitPostVerify      = 16
)

const (
	packageName   = "reasonix-desktop"
	dpkgDebPath   = "/usr/bin/dpkg-deb"
	dpkgQueryPath = "/usr/bin/dpkg-query"
	dpkgPath      = "/usr/bin/dpkg"
	aptGetPath    = "/usr/bin/apt-get"

	// maxInputBytes bounds untrusted package/signature files before they are
	// copied into the root temp directory (desktop .deb + minisig).
	maxInputBytes = 512 << 20 // 512 MiB

	// phasePrefix is a single-line protocol the desktop parses from stderr so
	// the UI can leave "authorizing" once Polkit has launched this helper and
	// validation finished, before apt-get starts.
	phasePrefix = "REASONIX_UPDATE_PHASE="
)

type helperResult struct {
	OK      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
}

// installDeps holds the privileged install seams. Production uses realDeps();
// tests inject fakes so every branch is deterministic without root/apt.
type installDeps struct {
	geteuid          func() int
	getenv           func(string) string
	mkTempDir        func() (string, error)
	removeAll        func(string) error
	copyOwnedRegular func(src, dst string, mode os.FileMode, ownerUID int, maxBytes int64) error
	readFile         func(string) ([]byte, error)
	verify           func(data, sig []byte) error
	inspectDeb       func(path string) (debIdentity, error)
	installedVersion func() (string, error)
	compareVersions  func(a, b string) (int, error)
	aptInstall       func(pkgPath string, allowDowngrade bool) error
	verifyInstalled  func(want string) error
	writePhase       func(phase string)
	writeResult      func(helperResult)
	goArch           string
	maxInputBytes    int64
}

func realDeps() installDeps {
	return installDeps{
		geteuid:          os.Geteuid,
		getenv:           os.Getenv,
		mkTempDir:        func() (string, error) { return os.MkdirTemp("", "reasonix-update-*") },
		removeAll:        os.RemoveAll,
		copyOwnedRegular: copyOwnedRegularFile,
		readFile:         os.ReadFile,
		verify:           update.Verify,
		inspectDeb:       inspectDeb,
		installedVersion: installedPackageVersion,
		compareVersions:  compareDebVersions,
		aptInstall:       aptInstallOnlyUpgrade,
		verifyInstalled:  verifyInstalled,
		writePhase:       writePhaseLine,
		writeResult:      writeResultJSON,
		goArch:           runtime.GOARCH,
		maxInputBytes:    maxInputBytes,
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	return runWith(realDeps(), args)
}

func runWith(d installDeps, args []string) int {
	if len(args) == 0 {
		d.writeResult(helperResult{OK: false, Error: "missing command", Code: "usage"})
		return exitUsage
	}
	switch args[0] {
	case "install":
		return runInstall(d, args[1:])
	default:
		d.writeResult(helperResult{OK: false, Error: "unknown command", Code: "usage"})
		return exitUsage
	}
}

func runInstall(d installDeps, args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var packagePath, signaturePath string
	fs.StringVar(&packagePath, "package", "", "path to the verified .deb")
	fs.StringVar(&signaturePath, "signature", "", "path to the detached .minisig")
	if err := fs.Parse(args); err != nil {
		d.writeResult(helperResult{OK: false, Error: "invalid arguments", Code: "usage"})
		return exitUsage
	}
	if packagePath == "" || signaturePath == "" || fs.NArg() != 0 {
		d.writeResult(helperResult{OK: false, Error: "install requires --package and --signature", Code: "usage"})
		return exitUsage
	}

	if d.geteuid() != 0 {
		d.writeResult(helperResult{OK: false, Error: "helper must run as root", Code: "not_root"})
		return exitNotRoot
	}
	pkUID, err := strconv.Atoi(strings.TrimSpace(d.getenv("PKEXEC_UID")))
	if err != nil || pkUID < 0 {
		d.writeResult(helperResult{OK: false, Error: "missing or invalid PKEXEC_UID", Code: "not_root"})
		return exitNotRoot
	}

	tmpDir, err := d.mkTempDir()
	if err != nil {
		d.writeResult(helperResult{OK: false, Error: "create temp dir failed", Code: "install_failed"})
		return exitInstallFailed
	}
	// Ensure root-only access before any untrusted bytes land here.
	if err := os.Chmod(tmpDir, 0o700); err != nil {
		_ = d.removeAll(tmpDir)
		d.writeResult(helperResult{OK: false, Error: "secure temp dir failed", Code: "install_failed"})
		return exitInstallFailed
	}
	defer func() { _ = d.removeAll(tmpDir) }()

	maxBytes := d.maxInputBytes
	if maxBytes <= 0 {
		maxBytes = maxInputBytes
	}
	pkgCopy := filepath.Join(tmpDir, "package.deb")
	sigCopy := filepath.Join(tmpDir, "package.deb.minisig")
	if err := d.copyOwnedRegular(packagePath, pkgCopy, 0o600, pkUID, maxBytes); err != nil {
		d.writeResult(helperResult{OK: false, Error: "invalid package input", Code: "bad_input"})
		return exitBadInput
	}
	if err := d.copyOwnedRegular(signaturePath, sigCopy, 0o600, pkUID, maxBytes); err != nil {
		d.writeResult(helperResult{OK: false, Error: "invalid signature input", Code: "bad_input"})
		return exitBadInput
	}

	pkgData, err := d.readFile(pkgCopy)
	if err != nil {
		d.writeResult(helperResult{OK: false, Error: "read package failed", Code: "bad_input"})
		return exitBadInput
	}
	sigData, err := d.readFile(sigCopy)
	if err != nil {
		d.writeResult(helperResult{OK: false, Error: "read signature failed", Code: "bad_input"})
		return exitBadInput
	}
	// Re-verify as root; never trust the unprivileged process's prior check.
	if err := d.verify(pkgData, sigData); err != nil {
		d.writeResult(helperResult{OK: false, Error: "signature verification failed", Code: "verify_failed"})
		return exitVerifyFailed
	}

	candidate, err := d.inspectDeb(pkgCopy)
	if err != nil {
		d.writeResult(helperResult{OK: false, Error: err.Error(), Code: "package_rejected"})
		return exitPackageRejected
	}
	if err := acceptDebIdentity(candidate, d.goArch); err != nil {
		d.writeResult(helperResult{OK: false, Error: err.Error(), Code: "package_rejected"})
		return exitPackageRejected
	}

	installed, err := d.installedVersion()
	if err != nil {
		d.writeResult(helperResult{OK: false, Error: err.Error(), Code: "package_rejected"})
		return exitPackageRejected
	}
	cmp, err := d.compareVersions(candidate.Version, installed)
	if err != nil {
		d.writeResult(helperResult{OK: false, Error: "version compare failed", Code: "package_rejected"})
		return exitPackageRejected
	}
	allowDowngrade, err := acceptVersionTransition(cmp, candidate.Version, installed)
	if err != nil {
		d.writeResult(helperResult{OK: false, Error: err.Error(), Code: "package_rejected"})
		return exitPackageRejected
	}

	// Polkit already authorized this process; validation is complete. Tell the
	// desktop to leave "authorizing" before the long apt-get call.
	d.writePhase("installing")

	if err := d.aptInstall(pkgCopy, allowDowngrade); err != nil {
		code := "install_failed"
		exit := exitInstallFailed
		if isPackageManagerBusy(err) {
			code = "package_manager_busy"
			exit = exitBusy
		}
		d.writeResult(helperResult{OK: false, Error: sanitizeHelperError(err), Code: code})
		return exit
	}

	if err := d.verifyInstalled(candidate.Version); err != nil {
		d.writeResult(helperResult{OK: false, Error: err.Error(), Code: "package_verify_failed"})
		return exitPostVerify
	}

	d.writeResult(helperResult{OK: true, Version: candidate.Version})
	return exitOK
}

type debIdentity struct {
	Package string
	Version string
	Arch    string
}

// acceptDebIdentity enforces package name and architecture. Pure for tests.
func acceptDebIdentity(id debIdentity, goArch string) error {
	if id.Package != packageName {
		return errors.New("package name rejected")
	}
	wantArch := goArch
	if wantArch == "386" {
		wantArch = "i386"
	}
	if id.Arch != wantArch && id.Arch != "all" {
		return errors.New("package architecture rejected")
	}
	if id.Version == "" {
		return errors.New("package version missing")
	}
	return nil
}

var (
	stableDebVersionRE  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	previewDebVersionRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)~preview\.(0|[1-9][0-9]*)$`)
)

// acceptVersionTransition permits ordinary upgrades and the one intentional
// downgrade: replacing a public Preview package with a public Stable package
// after the user switches channels.
func acceptVersionTransition(cmp int, candidate, installed string) (bool, error) {
	switch {
	case cmp > 0:
		return false, nil
	case cmp == 0:
		return false, errors.New("candidate version is not strictly newer")
	case stableDebVersionRE.MatchString(candidate) && previewDebVersionRE.MatchString(installed):
		return true, nil
	default:
		return false, errors.New("candidate downgrade is not an allowed preview-to-stable transition")
	}
}

// aptInstallArgv is the fixed absolute apt-get argv (never shell). Pure for tests.
func aptInstallArgv(pkgPath string, allowDowngrade bool) []string {
	argv := []string{
		aptGetPath,
		"install",
		"--assume-yes",
		"--only-upgrade",
		"--no-remove",
	}
	if allowDowngrade {
		argv = append(argv, "--allow-downgrades")
	}
	return append(argv, pkgPath)
}

func inspectDeb(path string) (debIdentity, error) {
	pkg, err := dpkgDebField(path, "Package")
	if err != nil {
		return debIdentity{}, errors.New("dpkg-deb inspection failed")
	}
	ver, err := dpkgDebField(path, "Version")
	if err != nil {
		return debIdentity{}, errors.New("dpkg-deb inspection failed")
	}
	arch, err := dpkgDebField(path, "Architecture")
	if err != nil {
		return debIdentity{}, errors.New("dpkg-deb inspection failed")
	}
	return debIdentity{Package: pkg, Version: ver, Arch: arch}, nil
}

func dpkgDebField(path, field string) (string, error) {
	out, err := exec.Command(dpkgDebPath, "-f", path, field).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func installedPackageVersion() (string, error) {
	out, err := exec.Command(dpkgQueryPath, "-W", "-f=${Version}", packageName).Output()
	if err != nil {
		return "", errors.New("installed package not found")
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", errors.New("installed package version empty")
	}
	return v, nil
}

// compareDebVersions returns >0 when a > b using dpkg --compare-versions.
func compareDebVersions(a, b string) (int, error) {
	if err := exec.Command(dpkgPath, "--compare-versions", a, "gt", b).Run(); err == nil {
		return 1, nil
	}
	if err := exec.Command(dpkgPath, "--compare-versions", a, "eq", b).Run(); err == nil {
		return 0, nil
	}
	if err := exec.Command(dpkgPath, "--compare-versions", a, "lt", b).Run(); err == nil {
		return -1, nil
	}
	return 0, errors.New("compare-versions failed")
}

func aptInstallOnlyUpgrade(pkgPath string, allowDowngrade bool) error {
	argv := aptInstallArgv(pkgPath, allowDowngrade)
	cmd := exec.Command(argv[0], argv[1:]...)
	// Fixed absolute argv only — never shell.
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func verifyInstalled(wantVersion string) error {
	out, err := exec.Command(dpkgQueryPath, "-W", "-f=${Status}\n${Version}", packageName).Output()
	if err != nil {
		return errors.New("post-install dpkg-query failed")
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return errors.New("post-install package state incomplete")
	}
	if strings.TrimSpace(lines[0]) != "install ok installed" {
		return errors.New("package not in install ok installed state")
	}
	if strings.TrimSpace(lines[1]) != wantVersion {
		return errors.New("installed version mismatch")
	}
	return nil
}

// copyOwnedRegularFile opens src with O_NOFOLLOW only (fail closed — no Lstat/Open
// fallback), requires a regular file owned by ownerUID, enforces maxBytes, and
// writes a root-owned copy at dst. Ownership is checked on the opened fd.
func copyOwnedRegularFile(src, dst string, mode os.FileMode, ownerUID int, maxBytes int64) error {
	f, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		// Fail closed: never fall back to a followable open (TOCTOU).
		return fmt.Errorf("open without following links: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return errors.New("input exceeds size bound")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("stat owner unavailable")
	}
	if int(st.Uid) != ownerUID {
		return errors.New("input owner does not match PKEXEC_UID")
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	// Cap the copy even if size grew after Stat (regular files can still be
	// truncated/appended by the owner on some filesystems; bound the bytes we take).
	limited := io.LimitReader(f, maxBytes+1)
	n, err := io.Copy(out, limited)
	if err != nil {
		return err
	}
	if maxBytes > 0 && n > maxBytes {
		return errors.New("input exceeds size bound")
	}
	return out.Close()
}

func isPackageManagerBusy(err error) bool {
	if err == nil {
		return false
	}
	low := strings.ToLower(err.Error())
	return strings.Contains(low, "could not get lock") ||
		strings.Contains(low, "unable to acquire the dpkg frontend lock") ||
		strings.Contains(low, "is another process using it") ||
		strings.Contains(low, "dpkg frontend lock")
}

// sanitizeHelperError strips absolute paths from helper diagnostics so the
// desktop UI never surfaces user home directories.
func sanitizeHelperError(err error) string {
	if err == nil {
		return "install failed"
	}
	msg := err.Error()
	// Drop anything that looks like an absolute path segment.
	fields := strings.Fields(msg)
	for i, f := range fields {
		if strings.HasPrefix(f, "/") {
			fields[i] = "<path>"
		}
	}
	out := strings.Join(fields, " ")
	if out == "" {
		return "install failed"
	}
	if len(out) > 240 {
		out = out[:240]
	}
	return out
}

func writePhaseLine(phase string) {
	// Single line, no user paths — desktop parses this while the helper runs.
	fmt.Fprintf(os.Stderr, "%s%s\n", phasePrefix, phase)
}

func writeResultJSON(r helperResult) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(r)
}

// parsePhaseLine extracts a progress phase from a helper stderr line.
func parsePhaseLine(line string) (phase string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, phasePrefix) {
		return "", false
	}
	phase = strings.TrimSpace(strings.TrimPrefix(line, phasePrefix))
	return phase, phase != ""
}
