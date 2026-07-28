package telemetry

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/netclient"
)

const endpoint = "https://crash.reasonix.io/v1"

var uploadSignals = map[string]bool{
	"finish_reason": true, "empty_final": true, "provider_error": true,
	"cache_hit": true, "tool_error": true, "compaction": true, "turns": true,
	"recovery_failure": true, "recovery_rule_continue": true,
	"recovery_review_continue": true, "recovery_human_prompt": true,
	"recovery_human_continue": true, "recovery_human_revise": true,
	"recovery_review_error": true, "recovery_repeat_prompt": true,
	"recovery_review_latency": true, "client_surface": true,
	"client_version": true, "settings_language": true, "cli_mode": true,
	"cli_profile": true, "cli_permission_mode": true, "cli_session_mode": true,
	"cli_turn_latency": true, "cli_exit": true,
}

type Client struct {
	home      string
	version   string
	installID string
	http      *http.Client
}

func newClient(home, version string, proxy netclient.ProxySpec) (*Client, error) {
	if strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("telemetry: empty home")
	}
	client, err := netclient.NewHTTPClient(proxy, netclient.TransportOptions{
		DialTimeout:           500 * time.Millisecond,
		TLSHandshakeTimeout:   500 * time.Millisecond,
		ResponseHeaderTimeout: 750 * time.Millisecond,
	})
	if err != nil {
		return nil, err
	}
	client.Timeout = time.Second
	id, err := installID(home)
	if err != nil {
		return nil, err
	}
	return &Client{home: home, version: version, installID: id, http: client}, nil
}

func installID(home string) (string, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(home, "cli-telemetry-install-id")
	if b, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(b))
		if validInstallID(id) {
			return id, nil
		}
		replacement, err := randomHex(16)
		if err != nil {
			return "", err
		}
		if err := fileutil.AtomicWriteFile(path, []byte(replacement+"\n"), 0o600); err != nil {
			return "", err
		}
		return replacement, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if b, readErr := os.ReadFile(path); readErr == nil && validInstallID(strings.TrimSpace(string(b))) {
			return strings.TrimSpace(string(b)), nil
		}
		return "", err
	}
	if _, err = io.WriteString(f, id+"\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return id, nil
}

func validInstallID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (c *Client) backgroundFlush() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = c.sendDailyPing(ctx)
	_ = c.flushPending(ctx)
}

func (c *Client) sendDailyPing(ctx context.Context) error {
	day := time.Now().UTC().Format("2006-01-02")
	claim := filepath.Join(c.home, "cli-telemetry-ping-"+day)
	f, err := os.OpenFile(claim, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			state, readErr := os.ReadFile(claim)
			if readErr == nil && strings.TrimSpace(string(state)) == "sent" {
				return nil
			}
			if info, statErr := os.Stat(claim); statErr == nil && time.Since(info.ModTime()) < 2*time.Minute {
				return nil
			}
			if os.Remove(claim) == nil {
				return c.sendDailyPing(ctx)
			}
		}
		return err
	}
	if _, err := io.WriteString(f, "sending\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(claim)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(claim)
		return err
	}
	err = c.post(ctx, "/ping", pingPayload{
		InstallID: c.installID, Version: c.version, OS: runtime.GOOS, Arch: runtime.GOARCH, Surface: "cli",
	})
	if err != nil {
		_ = os.Remove(claim)
	} else if writeErr := os.WriteFile(claim, []byte("sent\n"), 0o600); writeErr != nil {
		_ = os.Remove(claim)
		err = writeErr
	}
	c.prunePingClaims(day)
	return err
}

func (c *Client) prunePingClaims(current string) {
	entries, _ := os.ReadDir(c.home)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "cli-telemetry-ping-") && entry.Name() != "cli-telemetry-ping-"+current {
			_ = os.Remove(filepath.Join(c.home, entry.Name()))
		}
	}
}

func (c *Client) flushPending(ctx context.Context) error {
	dir := filepath.Join(c.home, pendingDirName)
	paths, err := claimPendingFiles(dir, time.Now())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	claimed := make(map[string]bool, len(paths))
	for _, path := range paths {
		claimed[path] = true
	}
	defer func() {
		for path, active := range claimed {
			if active {
				_ = os.Rename(path, strings.TrimSuffix(path, ".uploading"))
			}
		}
	}()
	type group struct {
		payload pendingPayload
		paths   []string
		counts  map[string]int
	}
	groups := map[string]*group{}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var p pendingPayload
		if json.Unmarshal(b, &p) != nil || !validPendingPayload(p) {
			removeClaim(path, claimed)
			continue
		}
		key := p.Version + "\x00" + p.OS
		g := groups[key]
		if g == nil {
			g = &group{payload: pendingPayload{Version: p.Version, OS: p.OS}, counts: map[string]int{}}
			groups[key] = g
		}
		g.paths = append(g.paths, path)
		for _, counter := range p.Counters {
			if validCounter(counter) {
				g.counts[counter.Signal+"\x00"+counter.Bucket] += counter.Count
			}
		}
	}
	for _, g := range groups {
		counters := make([]Counter, 0, len(g.counts))
		for key, count := range g.counts {
			signal, bucket, _ := strings.Cut(key, "\x00")
			if count > 1_000_000 {
				count = 1_000_000
			}
			counters = append(counters, Counter{Signal: signal, Bucket: bucket, Count: count})
		}
		sort.Slice(counters, func(i, j int) bool {
			if counters[i].Signal == counters[j].Signal {
				return counters[i].Bucket < counters[j].Bucket
			}
			return counters[i].Signal < counters[j].Signal
		})
		if len(counters) == 0 {
			for _, path := range g.paths {
				removeClaim(path, claimed)
			}
			continue
		}
		if err := c.post(ctx, "/metrics", metricsPayload{
			InstallID: c.installID, Version: g.payload.Version, OS: g.payload.OS, Surface: "cli", Counters: counters,
		}); err != nil {
			return err
		}
		for _, path := range g.paths {
			removeClaim(path, claimed)
		}
	}
	return nil
}

func removeClaim(path string, claimed map[string]bool) {
	if err := os.Remove(path); err == nil || os.IsNotExist(err) {
		claimed[path] = false
	}
}

func claimPendingFiles(dir string, now time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json.uploading") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < 2*time.Minute {
			continue
		}
		_ = os.Rename(path, strings.TrimSuffix(path, ".uploading"))
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var claimed []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		claim := path + ".uploading"
		if os.Rename(path, claim) == nil {
			claimed = append(claimed, claim)
		}
	}
	return claimed, nil
}

func validPendingPayload(p pendingPayload) bool {
	if !releaseVersionPattern.MatchString(strings.TrimSpace(p.Version)) || len(p.Version) > 64 {
		return false
	}
	switch p.OS {
	case "android", "darwin", "freebsd", "linux", "windows":
	default:
		return false
	}
	return len(p.Counters) > 0 && len(p.Counters) <= 128
}

func validCounter(c Counter) bool {
	return uploadSignals[c.Signal] && c.Count > 0 && c.Count <= 1_000_000 &&
		len(c.Bucket) > 0 && len(c.Bucket) <= 96 && !unsafeBucketChars.MatchString(c.Bucket)
}

func (c *Client) post(ctx context.Context, path string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry: HTTP %d", resp.StatusCode)
	}
	return nil
}
