package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	pendingDirName = "cli-telemetry-pending"
	maxPending     = 64
	maxPendingAge  = 14 * 24 * time.Hour
)

func Cleanup(home string) error {
	if strings.TrimSpace(home) == "" {
		return nil
	}
	err := os.RemoveAll(filepath.Join(home, pendingDirName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func appendPending(home string, p pendingPayload) error {
	if len(p.Counters) == 0 || strings.TrimSpace(home) == "" {
		return nil
	}
	dir := filepath.Join(home, pendingDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if !prunePending(dir, time.Now()) {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	nonce, err := randomHex(8)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%d-%s.json", time.Now().UnixNano(), os.Getpid(), nonce)
	tmp := filepath.Join(dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// prunePending removes expired entries and makes room for one new pending file.
// Active upload claims count toward the cap but are never removed; if every
// slot is actively claimed, the new sample is dropped instead of growing the
// queue without bound.
func prunePending(dir string, now time.Time) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	type item struct {
		path string
		mod  time.Time
	}
	items := make([]item, 0, len(entries))
	activeClaims := 0
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".json") && !strings.HasSuffix(entry.Name(), ".json.uploading")) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if now.Sub(info.ModTime()) > maxPendingAge {
			_ = os.Remove(path)
			continue
		}
		if strings.HasSuffix(path, ".json.uploading") {
			if now.Sub(info.ModTime()) < 2*time.Minute {
				activeClaims++
				continue
			}
			recovered := strings.TrimSuffix(path, ".uploading")
			if err := os.Rename(path, recovered); err != nil {
				activeClaims++
				continue
			}
			path = recovered
		}
		items = append(items, item{path: path, mod: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.Before(items[j].mod) })
	for len(items)+activeClaims >= maxPending && len(items) > 0 {
		_ = os.Remove(items[0].path)
		items = items[1:]
	}
	return len(items)+activeClaims < maxPending
}

func randomHex(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
