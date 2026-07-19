package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/config"
)

const (
	remoteWindowTicketArgPrefix = "--remote-window-ticket="
	remoteWindowTicketPrefix    = ".remote-window-"
	remoteWindowTicketTTL       = 2 * time.Minute
	remoteWindowTicketMaxBytes  = 16 * 1024
)

// remoteWindowLaunch is a one-shot handoff from the primary Reasonix process to
// a lightweight native window process. The URL contains the local tunnel token,
// so the descriptor lives in a mode-0600 file instead of the process arguments.
type remoteWindowLaunch struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

func remoteWindowTicketPath(ticket string) (string, error) {
	if ticket == "" || filepath.Base(ticket) != ticket || !strings.HasPrefix(ticket, remoteWindowTicketPrefix) {
		return "", fmt.Errorf("invalid remote window ticket")
	}
	dir := strings.TrimSpace(config.MemoryUserDir())
	if dir == "" {
		return "", fmt.Errorf("cannot resolve remote window state directory")
	}
	return filepath.Join(dir, ticket), nil
}

func writeRemoteWindowLaunch(launch remoteWindowLaunch) (string, error) {
	if !isSafeRemoteWindowURL(launch.URL) {
		return "", fmt.Errorf("remote window URL must use HTTP on loopback")
	}
	dir := config.MemoryUserDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create remote window state directory: %w", err)
	}
	f, err := os.CreateTemp(dir, remoteWindowTicketPrefix)
	if err != nil {
		return "", fmt.Errorf("create remote window ticket: %w", err)
	}
	path := f.Name()
	remove := true
	defer func() {
		_ = f.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return "", fmt.Errorf("secure remote window ticket: %w", err)
	}
	if err := json.NewEncoder(f).Encode(launch); err != nil {
		return "", fmt.Errorf("write remote window ticket: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync remote window ticket: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close remote window ticket: %w", err)
	}
	remove = false
	return filepath.Base(path), nil
}

func consumeRemoteWindowLaunch(ticket string) (*remoteWindowLaunch, error) {
	path, err := remoteWindowTicketPath(ticket)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect remote window ticket: %w", err)
	}
	defer os.Remove(path)
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("remote window ticket is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > remoteWindowTicketMaxBytes {
		return nil, fmt.Errorf("remote window ticket has an invalid size")
	}
	// Windows does not expose Unix owner/group permission bits through Stat;
	// CreateTemp still creates the file for the current user, while ACLs remain
	// governed by the private user state directory.
	if goruntime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("remote window ticket permissions are too broad")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read remote window ticket: %w", err)
	}
	var launch remoteWindowLaunch
	if err := json.Unmarshal(data, &launch); err != nil {
		return nil, fmt.Errorf("decode remote window ticket: %w", err)
	}
	if !isSafeRemoteWindowURL(launch.URL) {
		return nil, fmt.Errorf("remote window URL must use HTTP on loopback")
	}
	return &launch, nil
}

func isSafeRemoteWindowURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil {
		return false
	}
	host := strings.TrimSpace(u.Hostname())
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func remoteWindowNavigationJS(raw string) (string, error) {
	if !isSafeRemoteWindowURL(raw) {
		return "", fmt.Errorf("remote window URL must use HTTP on loopback")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	return "window.location.replace(" + string(encoded) + ");", nil
}

func spawnRemoteWindow(launch remoteWindowLaunch) error {
	ticket, err := writeRemoteWindowLaunch(launch)
	if err != nil {
		return err
	}
	path, _ := remoteWindowTicketPath(ticket)
	executable, err := os.Executable()
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("locate Reasonix executable: %w", err)
	}
	cmd := exec.Command(executable, remoteWindowTicketArgPrefix+ticket)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("start remote Reasonix window: %w", err)
	}
	// The child normally consumes the ticket immediately. This bounds any
	// leftover token file if the child exits before reaching argument parsing.
	time.AfterFunc(remoteWindowTicketTTL, func() { _ = os.Remove(path) })
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release remote Reasonix window process: %w", err)
	}
	return nil
}

func remoteWindowTitle(hostID string) string {
	hostID = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, hostID))
	runes := []rune(hostID)
	if len(runes) > 80 {
		hostID = string(runes[:80]) + "…"
	}
	if hostID == "" {
		hostID = "Remote"
	}
	return "Reasonix [SSH: " + hostID + "]"
}

func (a *App) openRemoteWindow(rawURL, hostID string) error {
	launch := remoteWindowLaunch{URL: rawURL, Title: remoteWindowTitle(hostID)}
	if a.remoteWindowOpener != nil {
		return a.remoteWindowOpener(launch)
	}
	return spawnRemoteWindow(launch)
}

func (a *App) remoteWindowAssetMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a.remoteWindow == nil || (r.URL.Path != "/" && r.URL.Path != "/index.html") {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
			_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><style>html{background:#1a1a2e}</style></head><body></body></html>`))
		})
	}
}

func (a *App) domReadyRemoteWindow() {
	if a.remoteWindow == nil {
		return
	}
	if a.remoteWindowNavigated.CompareAndSwap(false, true) {
		if a.remoteWindow.Title != "" {
			runtime.WindowSetTitle(a.ctx, a.remoteWindow.Title)
		}
		if js, err := remoteWindowNavigationJS(a.remoteWindow.URL); err == nil {
			runtime.WindowExecJS(a.ctx, js)
		}
	}
	runtime.WindowCenter(a.ctx)
	runtime.WindowShow(a.ctx)
}
