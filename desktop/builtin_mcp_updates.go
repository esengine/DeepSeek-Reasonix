package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/codegraph"
	"reasonix/internal/config"
)

var (
	checkCodegraphLatest    = codegraph.LatestVersionWithClient
	downloadLatestCodegraph = codegraph.DownloadLatestWithClient
	builtInMCPUpdateNow     = time.Now
)

type BuiltInMCPUpdateStatus struct {
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Phase   string `json:"phase"` // current | available | downloaded | activated | skipped | error
	Path    string `json:"path,omitempty"`
	Err     string `json:"err,omitempty"`
}

func (a *App) checkBuiltInMCPUpdates() {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	mode := cfg.BuiltInMCPUpdates.ResolvedMode()
	if mode == config.BuiltInMCPUpdateModeOff {
		return
	}
	if !shouldRunBuiltInMCPUpdateCheck(cfg.BuiltInMCPUpdates.CheckIntervalDuration()) {
		return
	}
	statuses, _ := a.runBuiltInMCPUpdateCheck(cfg)
	markBuiltInMCPUpdateChecked()
	for _, status := range statuses {
		a.recordBuiltInMCPUpdateStatus(status)
	}
}

func (a *App) runBuiltInMCPUpdateCheck(cfg *config.Config) ([]BuiltInMCPUpdateStatus, error) {
	if cfg == nil {
		cfg = config.Default()
	}
	mode := cfg.BuiltInMCPUpdates.ResolvedMode()
	current := codegraph.ActiveVersion()
	if current == "" {
		current = codegraph.Version
	}
	status := BuiltInMCPUpdateStatus{
		Name:    "codegraph",
		Mode:    mode,
		Current: current,
		Phase:   "skipped",
	}
	if mode == config.BuiltInMCPUpdateModeOff {
		return []BuiltInMCPUpdateStatus{status}, nil
	}
	client, err := httpClient()
	if err != nil {
		status.Phase = "error"
		status.Err = err.Error()
		return []BuiltInMCPUpdateStatus{status}, nil
	}
	manifestCtx, cancel := context.WithTimeout(a.reqCtx(), httpTimeout)
	defer cancel()
	latest, err := checkCodegraphLatest(manifestCtx, client)
	if err != nil {
		status.Phase = "error"
		status.Err = err.Error()
		return []BuiltInMCPUpdateStatus{status}, nil
	}
	status.Latest = latest
	if !codegraph.NewerThanActive(latest) {
		status.Phase = "current"
		return []BuiltInMCPUpdateStatus{status}, nil
	}
	switch mode {
	case config.BuiltInMCPUpdateModeDownload:
		res, err := downloadLatestCodegraph(a.reqCtx(), client, nil)
		if err != nil {
			status.Phase = "error"
			status.Err = err.Error()
			return []BuiltInMCPUpdateStatus{status}, nil
		}
		status.Latest = res.Version
		status.Path = res.Path
		status.Phase = "downloaded"
	case config.BuiltInMCPUpdateModeAutoNextSession:
		res, err := updateBuiltInCodegraph(a.reqCtx(), client, nil)
		if err != nil {
			status.Phase = "error"
			status.Err = err.Error()
			return []BuiltInMCPUpdateStatus{status}, nil
		}
		status.Latest = res.Version
		status.Path = res.Path
		status.Phase = "activated"
	default:
		status.Phase = "available"
	}
	return []BuiltInMCPUpdateStatus{status}, nil
}

func (a *App) emitBuiltInMCPUpdateStatus(status BuiltInMCPUpdateStatus) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "builtin-mcp:update", status)
}

func (a *App) recordBuiltInMCPUpdateStatus(status BuiltInMCPUpdateStatus) {
	if strings.TrimSpace(status.Name) == "" {
		return
	}
	a.builtInMCPUpdatesMu.Lock()
	if a.builtInMCPUpdates == nil {
		a.builtInMCPUpdates = map[string]BuiltInMCPUpdateStatus{}
	}
	a.builtInMCPUpdates[status.Name] = status
	a.builtInMCPUpdatesMu.Unlock()
	a.emitBuiltInMCPUpdateStatus(status)
}

func (a *App) BuiltInMCPUpdateStatuses() []BuiltInMCPUpdateStatus {
	a.builtInMCPUpdatesMu.RLock()
	defer a.builtInMCPUpdatesMu.RUnlock()
	out := make([]BuiltInMCPUpdateStatus, 0, len(a.builtInMCPUpdates))
	for _, status := range a.builtInMCPUpdates {
		out = append(out, status)
	}
	return out
}

func shouldRunBuiltInMCPUpdateCheck(interval time.Duration) bool {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	path, err := builtInMCPUpdateStampPath()
	if err != nil {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	last, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}
	return !builtInMCPUpdateNow().Before(last.Add(interval))
}

func markBuiltInMCPUpdateChecked() {
	path, err := builtInMCPUpdateStampPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(builtInMCPUpdateNow().Format(time.RFC3339Nano)+"\n"), 0o644)
}

func builtInMCPUpdateStampPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", fmt.Errorf("user cache dir is empty")
	}
	return filepath.Join(base, "reasonix", "builtin-mcp-updates", "last-check"), nil
}
