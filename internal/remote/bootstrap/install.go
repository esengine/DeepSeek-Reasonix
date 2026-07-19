package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/remote/sftpfs"
)

// ensureBinary resolves a usable reasonix binary on the remote host per the
// install strategy, returning its path and version. A located binary older
// than MinVersion counts as missing (it lacks --port-file/--token-file).
func ensureBinary(ctx context.Context, conn Conn, fs *sftpfs.FS, opts Options, home, goos, goarch string, paths StatePaths) (bin, version string, err error) {
	uploaded := uploadedBinPath(home)
	bin, version = locate(ctx, conn, uploaded, opts.MinVersion)
	if bin != "" {
		return bin, version, nil
	}

	strategy := opts.Install
	if strategy == "" {
		strategy = InstallAuto
	}
	opts.progress("install", strategy)

	switch strategy {
	case InstallNever:
		return "", "", fmt.Errorf("bootstrap: reasonix not found on remote and serve_install = never")
	case InstallNPM:
		return installViaNPM(ctx, conn, opts.MinVersion)
	case InstallUpload:
		return installViaUpload(ctx, conn, fs, opts, home, goos, goarch, uploaded)
	default: // auto: try npm, then upload
		if b, v, nerr := installViaNPM(ctx, conn, opts.MinVersion); nerr == nil {
			return b, v, nil
		} else if opts.LocalBinary == "" {
			return "", "", fmt.Errorf("%w; bootstrap: no local Reasonix CLI is available for upload", nerr)
		}
		return installViaUpload(ctx, conn, fs, opts, home, goos, goarch, uploaded)
	}
}

// locate finds an existing reasonix and returns it only if its serve command
// supports --port-file (the bootstrap contract). A binary that lacks the flag —
// including every currently-released version — is reported as missing so the
// install/upload path replaces it. minVersion is accepted for signature
// stability but the flag probe is authoritative.
func locate(ctx context.Context, conn Conn, uploaded, minVersion string) (bin, version string) {
	_ = minVersion
	res, err := conn.Exec(ctx, LocateCommand(uploaded))
	if err != nil {
		return "", ""
	}
	lines := strings.Split(strings.TrimRight(string(res.Stdout), "\n"), "\n")
	path := strings.TrimSpace(lines[0])
	if path == "" {
		return "", ""
	}
	supportsPortFile := false
	for _, ln := range lines[1:] {
		ln = strings.TrimSpace(ln)
		if ln == "portfile:yes" {
			supportsPortFile = true
		} else if ln == "portfile:no" {
			supportsPortFile = false
		} else if v, verr := ParseVersion(ln); verr == nil {
			version = v
		}
	}
	if !supportsPortFile {
		// Missing the --port-file flag: treat as unusable so it is upgraded.
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
	loc, ver := locate(ctx, conn, "", minVersion)
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
	if err := fs.MkdirAll(ctx, dirOf(uploaded)); err != nil {
		return "", "", err
	}
	if err := fs.WriteFileAtomic(ctx, uploaded, data, 0o755); err != nil {
		return "", "", fmt.Errorf("bootstrap: upload binary: %w", err)
	}
	loc, ver := locate(ctx, conn, uploaded, opts.MinVersion)
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
