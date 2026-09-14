package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/remote/sftpfs"
)

// ensureBinary resolves a usable reasonix binary on the remote host per the
// install strategy, returning its path, its version, and a best-effort
// rollback for an installed package that later fails to launch. ForceUpgrade
// skips the locate fast-path so the install ladder replaces the binary.
func ensureBinary(ctx context.Context, conn Conn, fs *sftpfs.FS, opts Options, home, goos, goarch string, paths StatePaths) (bin, version string, rollback func(context.Context), err error) {
	uploaded := uploadedBinPath(home)
	strategy := opts.Install
	if strategy == "" {
		strategy = InstallAuto
	}
	if !opts.ForceUpgrade {
		bin, version = locate(ctx, conn, uploaded, opts.MinVersion)
		if bin != "" {
			return bin, version, nil, nil
		}
	} else {
		if strategy == InstallNever {
			// Refuse before touching the managed binary: renaming it aside
			// here would leave a never-install host with nothing to relaunch.
			return "", "", nil, fmt.Errorf("bootstrap: serve_install = never forbids upgrading the remote binary")
		}
		backupManagedBinary(ctx, fs, uploaded)
	}
	opts.progress("install", strategy)

	switch strategy {
	case InstallNever:
		return "", "", nil, fmt.Errorf("bootstrap: reasonix not found on remote and serve_install = never")
	case InstallNPM:
		if opts.ForceUpgrade {
			rollback = npmRollback(ctx, conn)
			bin, version, err = installViaNPMAt(ctx, conn, opts.ProductVersion)
		} else {
			bin, version, err = installViaNPM(ctx, conn, opts.MinVersion)
		}
	case InstallUpload:
		bin, version, err = installViaUpload(ctx, conn, fs, opts, home, goos, goarch, uploaded)
	default: // auto: try npm, packaged same-platform upload, then verified release upload
		if opts.ForceUpgrade {
			rollback, bin, version, err = upgradeLadder(ctx, conn, fs, opts, home, goos, goarch, uploaded)
		} else {
			bin, version, err = installLadder(ctx, conn, fs, opts, home, goos, goarch, uploaded)
		}
	}
	if err != nil {
		if opts.ForceUpgrade {
			restoreManagedBackupAt(ctx, fs, uploaded)
		}
		if rollback != nil {
			runRollback(rollback)
		}
		return "", "", nil, err
	}
	if opts.ForceUpgrade && !upgradeTargetMet(version, opts.ProductVersion) {
		restoreManagedBackupAt(ctx, fs, uploaded)
		if rollback != nil {
			runRollback(rollback)
		}
		return "", "", nil, fmt.Errorf("bootstrap: remote binary is %q after upgrade, desktop requires %q", version, opts.ProductVersion)
	}
	return bin, version, rollback, nil
}

// npmVersionSpec maps a desktop version to its published npm package spec:
// the release pipeline publishes preview prereleases as canary builds
// (scripts/resolve-preview-release.sh), so a pinned preview install must
// request the canary spec.
func npmVersionSpec(version string) string {
	base, suffix, ok := strings.Cut(version, "-")
	if !ok || !strings.HasPrefix(suffix, "preview.") {
		return version
	}
	return base + "-canary." + strings.TrimPrefix(suffix, "preview.")
}

// npmRollback captures the current global package version so a replacement
// that later fails its health check can be rolled back with a pinned
// reinstall. nil when no capable global binary exists to return to.
func npmRollback(ctx context.Context, conn Conn) func(context.Context) {
	bin, version := locateNPMGlobal(ctx, conn, "")
	if bin == "" || version == "" {
		return nil
	}
	return func(rollbackCtx context.Context) {
		_, _ = conn.Exec(rollbackCtx, fmt.Sprintf("npm i -g reasonix@%s 2>&1", npmVersionSpec(version)))
	}
}

// installLadder is the auto install order: npm, packaged same-platform upload,
// then verified release download.
func installLadder(ctx context.Context, conn Conn, fs *sftpfs.FS, opts Options, home, goos, goarch, uploaded string) (bin, version string, err error) {
	if b, v, nerr := installViaNPM(ctx, conn, opts.MinVersion); nerr == nil {
		return b, v, nil
	} else {
		attempts := []error{nerr}
		if opts.LocalBinary != "" && opts.LocalGOOS == goos && opts.LocalGOARCH == goarch {
			if b, v, uploadErr := installViaUpload(ctx, conn, fs, opts, home, goos, goarch, uploaded); uploadErr == nil {
				return b, v, nil
			} else {
				attempts = append(attempts, uploadErr)
			}
		} else if opts.LocalBinary == "" {
			attempts = append(attempts, errors.New("bootstrap: no local Reasonix CLI is available for upload"))
		} else {
			attempts = append(attempts, fmt.Errorf("bootstrap: local binary is %s/%s but remote is %s/%s", opts.LocalGOOS, opts.LocalGOARCH, goos, goarch))
		}
		if opts.FetchBinary != nil {
			binary, fetchErr := opts.FetchBinary(ctx, opts.ProductVersion, goos, goarch)
			if fetchErr == nil {
				if b, v, uploadErr := installBinaryBytes(ctx, conn, fs, binary, opts.MinVersion, home, uploaded); uploadErr == nil {
					return b, v, nil
				} else {
					attempts = append(attempts, uploadErr)
				}
			} else {
				attempts = append(attempts, fmt.Errorf("bootstrap: fetch official %s/%s CLI: %w", goos, goarch, fetchErr))
			}
		}
		return "", "", fmt.Errorf("bootstrap: automatic install failed: %w", errors.Join(attempts...))
	}
}

// backupManagedBinary renames the managed binary aside so a failed launch can
// restore the previous one; nothing happens when no binary exists.
func backupManagedBinary(ctx context.Context, fs *sftpfs.FS, uploaded string) {
	bctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = fs.Rename(bctx, uploaded, uploaded+".prev")
}

// upgradeLadder is the ForceUpgrade auto order: exact-release sources first -
// same-platform upload of the desktop's binary, then the official download at
// ProductVersion; npm pinned to that version is the fallback. A rung that
// lands below the target counts as failed, so later rungs still run; when npm
// wins, the pre-upgrade managed binary is restored so a later locate cannot
// adopt a rejected short upload. The returned rollback covers the npm package
// a failed launch must give back.
func upgradeLadder(ctx context.Context, conn Conn, fs *sftpfs.FS, opts Options, home, goos, goarch, uploaded string) (rollback func(context.Context), bin, version string, err error) {
	var attempts []error
	accept := func(b, v string, rungErr error, source string) bool {
		switch {
		case rungErr != nil:
			attempts = append(attempts, rungErr)
		case !upgradeTargetMet(v, opts.ProductVersion):
			attempts = append(attempts, fmt.Errorf("bootstrap: %s landed on %q, short of %q", source, v, opts.ProductVersion))
		default:
			return true
		}
		return false
	}
	if opts.LocalBinary != "" && opts.LocalGOOS == goos && opts.LocalGOARCH == goarch {
		b, v, uploadErr := installViaUpload(ctx, conn, fs, opts, home, goos, goarch, uploaded)
		if accept(b, v, uploadErr, "upload") {
			return nil, b, v, nil
		}
	} else if opts.LocalBinary == "" {
		attempts = append(attempts, errors.New("bootstrap: no local Reasonix CLI is available for upload"))
	} else {
		attempts = append(attempts, fmt.Errorf("bootstrap: local binary is %s/%s but remote is %s/%s", opts.LocalGOOS, opts.LocalGOARCH, goos, goarch))
	}
	if opts.FetchBinary != nil {
		binary, fetchErr := opts.FetchBinary(ctx, opts.ProductVersion, goos, goarch)
		if fetchErr == nil {
			b, v, uploadErr := installBinaryBytes(ctx, conn, fs, binary, opts.MinVersion, home, uploaded)
			if accept(b, v, uploadErr, "release download") {
				return nil, b, v, nil
			}
		} else {
			attempts = append(attempts, fmt.Errorf("bootstrap: fetch official %s/%s CLI: %w", goos, goarch, fetchErr))
		}
	}
	rollback = npmRollback(ctx, conn)
	b, v, nerr := installViaNPMAt(ctx, conn, opts.ProductVersion)
	if accept(b, v, nerr, "npm") {
		// The winning package lives outside the managed path; put the
		// pre-upgrade binary back so a future locate cannot adopt a rejected
		// short upload left there.
		restoreManagedBackupAt(ctx, fs, uploaded)
		return rollback, b, v, nil
	}
	return rollback, "", "", fmt.Errorf("bootstrap: forced upgrade install failed: %w", errors.Join(attempts...))
}

// runRollback gives a package rollback its own bounded context: npm installs
// routinely outlive the short cleanup contexts callers use.
func runRollback(rollback func(context.Context)) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rollback(ctx)
}

// upgradeTargetMet reports whether an upgrade landed on the desktop's release;
// a dev or unparseable ProductVersion carries no target to check.
func upgradeTargetMet(version, product string) bool {
	target, err := ParseVersion(product)
	if err != nil {
		return true
	}
	return version != "" && CompareVersions(version, target) >= 0
}

// locate finds an existing reasonix and returns it only if its serve command
// supports --port-file (the bootstrap contract). A binary that lacks the flag —
// including every currently-released version — is reported as missing so the
// install/upload path replaces it. minVersion is accepted for signature
// stability but the flag probe is authoritative.
func locate(ctx context.Context, conn Conn, uploaded, minVersion string) (bin, version string) {
	return locateWithCommand(ctx, conn, LocateCommand(uploaded), minVersion)
}

func locateUploaded(ctx context.Context, conn Conn, uploaded, minVersion string) (bin, version string) {
	return locateWithCommand(ctx, conn, LocateUploadedCommand(uploaded), minVersion)
}

func locateNPMGlobal(ctx context.Context, conn Conn, minVersion string) (bin, version string) {
	return locateWithCommand(ctx, conn, LocateNPMGlobalCommand(), minVersion)
}

func locateWithCommand(ctx context.Context, conn Conn, command, minVersion string) (bin, version string) {
	_ = minVersion
	res, err := conn.Exec(ctx, command)
	if err != nil {
		return "", ""
	}
	lines := strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n")
	path := strings.TrimSpace(lines[0])
	if path == "" {
		return "", ""
	}
	supportsPortFile, supportsSessionEvents, supportsDetachedHeal, supportsCaps := false, false, false, false
	for _, ln := range lines[1:] {
		ln = strings.TrimSpace(ln)
		if ln == "portfile:yes" {
			supportsPortFile = true
		} else if ln == "portfile:no" {
			supportsPortFile = false
		} else if ln == "sessionevents:yes" {
			supportsSessionEvents = true
		} else if ln == "sessionevents:no" {
			supportsSessionEvents = false
		} else if ln == "detachedheal:yes" {
			supportsDetachedHeal = true
		} else if ln == "detachedheal:no" {
			supportsDetachedHeal = false
		} else if ln == "caps:yes" {
			supportsCaps = true
		} else if ln == "caps:no" {
			supportsCaps = false
		} else if v, verr := ParseVersion(ln); verr == nil {
			version = v
		}
	}
	if !supportsPortFile || !supportsSessionEvents || !supportsDetachedHeal || !supportsCaps {
		// Missing a required Serve contract: treat as unusable so it is upgraded.
		return "", ""
	}
	return path, version
}

func installViaNPM(ctx context.Context, conn Conn, minVersion string) (bin, version string, err error) {
	res, err := conn.Exec(ctx, "npm i -g reasonix 2>&1")
	if err != nil {
		return "", "", fmt.Errorf("bootstrap: npm install: %w", err)
	}
	if res.ExitCode != 0 {
		return "", "", fmt.Errorf("bootstrap: npm install failed: %s", tail(res.Stdout, 400))
	}
	// npm may install outside the login PATH; probe npm prefix explicitly.
	loc, ver := locateNPMGlobal(ctx, conn, minVersion)
	if loc == "" {
		return "", "", fmt.Errorf("bootstrap: reasonix not found after npm install (check remote PATH / npm prefix)")
	}
	return loc, ver, nil
}

// installViaNPMAt installs an exact published version when the desktop
// carries one; anything else falls back to latest. Preview prereleases are
// translated to their published canary spec.
func installViaNPMAt(ctx context.Context, conn Conn, product string) (bin, version string, err error) {
	target, perr := ParseVersion(product)
	if perr != nil {
		return installViaNPM(ctx, conn, "")
	}
	spec := npmVersionSpec(target)
	res, err := conn.Exec(ctx, fmt.Sprintf("npm i -g reasonix@%s 2>&1", spec))
	if err != nil {
		return "", "", fmt.Errorf("bootstrap: npm install: %w", err)
	}
	if res.ExitCode != 0 {
		return "", "", fmt.Errorf("bootstrap: npm install reasonix@%s failed: %s", spec, tail(res.Stdout, 400))
	}
	loc, ver := locateNPMGlobal(ctx, conn, "")
	if loc == "" {
		return "", "", fmt.Errorf("bootstrap: reasonix not found after npm install (check remote PATH / npm prefix)")
	}
	return loc, ver, nil
}

// installViaUpload uploads the local reasonix binary when the remote platform
// matches the local one. Cross-platform release download is a documented V1
// limitation: use serve_install = npm for a differing remote platform.
func installViaUpload(ctx context.Context, conn Conn, fs *sftpfs.FS, opts Options, home, goos, goarch, uploaded string) (bin, version string, err error) {
	if opts.LocalBinary == "" {
		return "", "", fmt.Errorf("bootstrap: upload strategy needs the local reasonix binary path")
	}
	if opts.LocalGOOS != goos || opts.LocalGOARCH != goarch {
		return "", "", fmt.Errorf("bootstrap: cannot upload: local binary is %s/%s but remote is %s/%s; use serve_install = npm",
			opts.LocalGOOS, opts.LocalGOARCH, goos, goarch)
	}
	data, rerr := os.ReadFile(opts.LocalBinary)
	if rerr != nil {
		return "", "", fmt.Errorf("bootstrap: read local binary: %w", rerr)
	}
	return installBinaryBytes(ctx, conn, fs, data, opts.MinVersion, home, uploaded)
}

func installBinaryBytes(ctx context.Context, conn Conn, fs *sftpfs.FS, data []byte, minVersion, home, uploaded string) (bin, version string, err error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("bootstrap: downloaded binary is empty")
	}
	if err := fs.MkdirAll(ctx, dirOf(uploaded)); err != nil {
		return "", "", err
	}
	if err := fs.WriteFileAtomic(ctx, uploaded, data, 0o755); err != nil {
		return "", "", fmt.Errorf("bootstrap: upload binary: %w", err)
	}
	loc, ver := locateUploaded(ctx, conn, uploaded, minVersion)
	if loc == "" {
		return "", "", fmt.Errorf("bootstrap: uploaded binary not runnable on remote")
	}
	return loc, ver, nil
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}

func tail(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}
