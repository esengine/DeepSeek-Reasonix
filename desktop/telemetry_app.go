package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
)

// telemetry_app.go is the anonymous launch ping: one POST per app start carrying a
// random install id, version, and OS facts — never conversation, key, or file data.
// Gated on config desktop.telemetry (default on) and skipped entirely in dev builds.

var pingEndpoint = "https://crash.reasonix.io/v1/ping"

var installIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

var installIDRandRead = rand.Read

type startupPing struct {
	InstallID      string `json:"installId"`
	Version        string `json:"version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Channel        string `json:"channel,omitempty"`
	OSVersion      string `json:"osVersion,omitempty"`
	OSBuild        int    `json:"osBuild,omitempty"`
	OSRevision     int    `json:"osRevision,omitempty"`
	DistroID       string `json:"distroId,omitempty"`
	DistroVersion  string `json:"distroVersion,omitempty"`
	KernelVersion  string `json:"kernelVersion,omitempty"`
	SessionType    string `json:"sessionType,omitempty"`
	RuntimeEngine  string `json:"runtimeEngine,omitempty"`
	RuntimeVersion string `json:"runtimeVersion,omitempty"`
	GPUMode        string `json:"gpuMode,omitempty"`
}

func installID() (string, error) {
	path := filepath.Join(config.MemoryUserDir(), "install-id")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return "", err
	}
	defer release()

	if b, err := readFileUTF8(path); err == nil {
		if id := string(bytes.TrimSpace(b)); installIDPattern.MatchString(id) {
			return id, nil
		}
	}
	raw := make([]byte, 16)
	if _, err := installIDRandRead(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)
	if err := fileutil.AtomicWriteFile(path, []byte(id+"\n"), 0o644); err != nil {
		return "", err
	}
	return id, nil
}

func (a *App) sendStartupPing() {
	if version == "dev" {
		return
	}
	cfg, err := config.Load()
	if err != nil || !cfg.DesktopTelemetry() {
		return
	}
	id, err := installID()
	if err != nil {
		return
	}
	c, err := httpClient()
	if err != nil {
		return
	}
	device := collectDeviceInfo()
	runtimeContext := webRuntimeContextForTelemetry(500 * time.Millisecond)
	_ = postStartupPing(a.bootContext(), c, pingEndpoint, startupPing{
		InstallID: id, Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH, Channel: channel,
		OSVersion: device.OSVersion, OSBuild: device.OSBuild, OSRevision: device.OSRevision,
		DistroID: device.DistroID, DistroVersion: device.DistroVersion,
		KernelVersion: device.KernelVersion, SessionType: device.SessionType,
		RuntimeEngine: runtimeContext.Engine, RuntimeVersion: runtimeContext.RuntimeVersion,
		GPUMode: runtimeContext.GPUMode,
	})
}

func postStartupPing(ctx context.Context, c *http.Client, endpoint string, p startupPing) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
