package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"reasonix/internal/remote/sftpfs"
	"reasonix/internal/store"
)

// browserRelayMaxBytes bounds one staged capture; screenshots and downloads
// the shell writes are far smaller, and the HTTP wire already caps replies.
const browserRelayMaxBytes = 32 << 20

// FileRelay stages a desktop-side capture file (screenshot, download) onto
// the remote host through the existing SFTP channel, so the remote serve's
// tools read a path local to them. A desktop path is never handed to the
// remote as-is.
type FileRelay interface {
	// Stage copies localPath into the workspace's remote scratch directory
	// and returns the remote path of the copy.
	Stage(ctx context.Context, workspace, localPath string) (remotePath string, err error)
}

type browserUploadRelay interface {
	Fetch(ctx context.Context, workspace, remotePath, localDirectory string) (string, error)
}

// Fetch stages a remote-owned file on the desktop before the local browser
// sees a path. Resolve both source and roots on the remote filesystem; a
// remote absolute filename must never be interpreted by os.Open locally.
func (r sftpFileRelay) Fetch(ctx context.Context, workspace, remotePath, localDirectory string) (string, error) {
	fs, err := r.conn.SFTP()
	if err != nil {
		return "", err
	}
	home, homeErr := fs.RealPath(ctx, "~")
	wirePath := relaySFTPPath(remotePath, home)
	if !path.IsAbs(wirePath) && !(relayWindowsHome(home) && relayWindowsDrivePath(wirePath)) {
		return "", fmt.Errorf("browser upload: remote path must be absolute")
	}
	resolved, err := fs.RealPath(ctx, wirePath)
	if err != nil {
		return "", err
	}
	roots := []string{}
	if workspace != "" {
		roots = append(roots, relaySFTPPath(workspace, home))
	}
	if homeErr == nil {
		roots = append(roots, path.Join(home, ".reasonix", "browser-relay", store.RemoteWorkspaceSlug(workspace)))
	}
	owned := false
	for _, root := range roots {
		canonical, err := fs.RealPath(ctx, root)
		if err == nil && canonical != "/" && strings.HasPrefix(resolved, strings.TrimSuffix(canonical, "/")+"/") {
			owned = true
			break
		}
	}
	if !owned {
		return "", fmt.Errorf("browser upload: remote file is outside this task's workspace and scratch directory")
	}
	info, err := fs.Stat(ctx, resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode.IsRegular() || info.Size > browserRelayMaxBytes {
		return "", fmt.Errorf("browser upload: remote file must be regular and at most %d bytes", browserRelayMaxBytes)
	}
	dir, err := os.MkdirTemp(localDirectory, "remote-")
	if err != nil {
		return "", err
	}
	destination := filepath.Join(dir, path.Base(resolved))
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	_, copyErr := fs.Download(ctx, resolved, &browserUploadWriter{writer: f, remaining: browserRelayMaxBytes})
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("browser upload: remote staging failed: %w", errors.Join(copyErr, closeErr))
	}
	return destination, nil
}

type browserUploadWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *browserUploadWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("browser upload exceeds %d bytes", browserRelayMaxBytes)
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

// sftpConn is the slice of an SSH client the relay needs; desktopSSHClient
// satisfies it and tests fake it.
type sftpConn interface {
	SFTP() (*sftpfs.FS, error)
}

// sftpFileRelay is the FileRelay over one SSH connection generation.
type sftpFileRelay struct {
	conn sftpConn
}

func (r sftpFileRelay) Stage(ctx context.Context, workspace, localPath string) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("browser relay: stat %s: %w", localPath, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("browser relay: %s is not a regular file", localPath)
	}
	if info.Size() > browserRelayMaxBytes {
		return "", fmt.Errorf("browser relay: %s exceeds %d bytes", localPath, browserRelayMaxBytes)
	}
	fs, err := r.conn.SFTP()
	if err != nil {
		return "", fmt.Errorf("browser relay: sftp: %w", err)
	}
	home, err := fs.RealPath(ctx, "~")
	if err != nil {
		return "", fmt.Errorf("browser relay: resolve remote home: %w", err)
	}
	dir := path.Join(home, ".reasonix", "browser-relay", store.RemoteWorkspaceSlug(workspace))
	if err := fs.MkdirAll(ctx, dir); err != nil {
		return "", fmt.Errorf("browser relay: remote scratch dir: %w", err)
	}
	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("browser relay: open %s: %w", localPath, err)
	}
	defer f.Close()
	remote := path.Join(dir, relayFileName(localPath))
	if _, err := fs.UploadAtomic(ctx, remote, io.LimitReader(f, browserRelayMaxBytes), 0o600); err != nil {
		return "", fmt.Errorf("browser relay: upload %s: %w", localPath, err)
	}
	return relayAgentPath(remote, home), nil
}

// SFTP servers on Windows can canonicalize a drive as /C:/... while tools in
// the remote Agent need C:/... . Infer this from the remote canonical home,
// never from the desktop OS: a Windows desktop also connects to POSIX hosts.
// Keep wire paths in the server's spelling for every SFTP operation.
func relayAgentPath(wirePath, home string) string {
	if relayWindowsHome(home) && strings.HasPrefix(wirePath, "/") && relayWindowsDrivePath(wirePath[1:]) {
		return wirePath[1:]
	}
	return wirePath
}

func relaySFTPPath(agentPath, home string) string {
	if !relayWindowsHome(home) {
		return agentPath
	}
	normalized := strings.ReplaceAll(agentPath, "\\", "/")
	drivePath := strings.TrimPrefix(normalized, "/")
	if !relayWindowsDrivePath(drivePath) {
		return normalized
	}
	if strings.HasPrefix(home, "/") {
		return "/" + drivePath
	}
	return drivePath
}

func relayWindowsHome(home string) bool {
	return relayWindowsDrivePath(strings.TrimPrefix(home, "/"))
}

func relayWindowsDrivePath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && value[2] == '/'
}

// relayFileName prefixes the capture's base name with randomness so repeated
// captures of the same tab never overwrite a file a tool call still reads.
func relayFileName(localPath string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	base := filepath.Base(localPath)
	base = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '-'
		}
		return r
	}, base)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "capture"
	}
	return hex.EncodeToString(buf) + "-" + base
}
