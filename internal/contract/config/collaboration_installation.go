package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
)

const installationCollaborationFile = "collaboration-default-v1.json"
const installationCollaborationLock = "collaboration-default-v1.lock"
const unavailableInstallationCollaboration = "unsupported_installation_default"

type installationCollaboration struct {
	Version int    `json:"version"`
	Mode    string `json:"mode"`
}

func (r Roots) installationCollaboration(persist bool) (string, error) {
	if r.hasUserCollaborationConfiguration() {
		return "legacy", nil
	}
	path := filepath.Join(r.Home(), installationCollaborationFile)
	if mode, exists, err := readInstallationCollaboration(path); exists || err != nil {
		return mode, err
	}
	if !persist {
		fresh := r.freshCollaborationProfile()
		// A peer can publish its marker and history while this reader scans.
		if mode, exists, err := readInstallationCollaboration(path); exists || err != nil {
			return mode, err
		}
		if fresh {
			return "auto", nil
		}
		return "legacy", nil
	}
	if err := os.MkdirAll(r.Home(), 0700); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := filelock.Acquire(ctx, filepath.Join(r.Home(), installationCollaborationLock))
	if err != nil {
		return "", err
	}
	defer release()
	if mode, exists, err := readInstallationCollaboration(path); exists || err != nil {
		return mode, err
	}
	if !r.freshCollaborationProfile() {
		return "legacy", nil
	}
	body, err := json.Marshal(installationCollaboration{Version: 1, Mode: "auto"})
	if err != nil {
		return "", err
	}
	if err := fileutil.AtomicCreateFileStrict(path, append(body, '\n'), 0600); err != nil {
		if mode, exists, readErr := readInstallationCollaboration(path); exists && readErr == nil {
			return mode, nil
		}
		return "", err
	}
	return "auto", nil
}

func readInstallationCollaboration(path string) (string, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8192 {
		return "", true, fmt.Errorf("unsupported installation default at %s; original retained", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", true, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var policy installationCollaboration
	if decoder.Decode(&policy) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) || policy.Version != 1 || (policy.Mode != "auto" && policy.Mode != "legacy") {
		return "", true, fmt.Errorf("unsupported installation default at %s; original retained", path)
	}
	return policy.Mode, true, nil
}

func (r Roots) freshCollaborationProfile() bool {
	if r.hasUserCollaborationConfiguration() {
		return false
	}
	home, err := fileutil.CanonicalWritePath(r.Home())
	if err != nil || profileHasUserData(home) {
		return false
	}
	state, err := fileutil.CanonicalWritePath(r.Dir(RootState))
	return err == nil && (samePath(home, state) || !profileHasUserData(state))
}

func (r Roots) hasUserCollaborationConfiguration() bool {
	for _, path := range r.userConfigCandidatePaths() {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func profileHasUserData(root string) bool {
	hasData := false
	entries := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) && path == root {
			return fs.SkipAll
		}
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		entries++
		if entries > 50_000 {
			hasData = true
			return fs.SkipAll
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !strings.ContainsAny(relative, "/\\") {
			switch relative {
			case "logs", "cache", "tmp", "run", "telemetry":
				if entry.IsDir() {
					return fs.SkipDir
				}
			}
			if relative == installationCollaborationLock || strings.HasPrefix(relative, ".atomic-") && strings.HasSuffix(relative, ".tmp") {
				return nil
			}
		}
		if !entry.IsDir() {
			hasData = true
			return fs.SkipAll
		}
		return nil
	})
	return hasData || err != nil
}

func applyInstallationCollaboration(cfg *Config, mode string, err error) {
	if err != nil {
		if cfg.Agent.Collaboration != unavailableInstallationCollaboration {
			cfg.addLoadWarning("installation collaboration default unavailable: " + err.Error())
		}
		cfg.Agent.Collaboration = unavailableInstallationCollaboration
		return
	}
	if mode == "auto" {
		cfg.Agent.Collaboration = "auto"
	} else {
		cfg.Agent.Collaboration = ""
	}
}
