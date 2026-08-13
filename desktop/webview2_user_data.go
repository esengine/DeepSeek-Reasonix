package main

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
)

const (
	reasonixHomeEnv      = "REASONIX_HOME"
	reasonixStateHomeEnv = "REASONIX_STATE_HOME"
	reasonixCacheHomeEnv = "REASONIX_CACHE_HOME"
)

// webview2UserDataPath keeps explicitly isolated Reasonix runtimes from
// sharing Wails' executable-name-based default WebView2 profile. An ordinary
// install keeps the Wails default so existing user data remains unchanged.
func webview2UserDataPath() string {
	var root string
	switch {
	case explicitRuntimeDir(reasonixCacheHomeEnv), explicitRuntimeDir(reasonixHomeEnv):
		root = config.CacheDir()
	case explicitRuntimeDir(reasonixStateHomeEnv):
		root = config.MemoryUserDir()
	default:
		return ""
	}
	if strings.TrimSpace(root) == "" {
		return ""
	}
	return filepath.Join(root, "webview2")
}

func explicitRuntimeDir(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) != ""
}
