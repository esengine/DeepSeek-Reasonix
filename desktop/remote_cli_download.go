package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/releaseasset"
)

const remoteCLIDownloadTimeout = 2 * time.Minute

func downloadRemoteCLIBinary(ctx context.Context, version, goos, goarch string) ([]byte, error) {
	if version == "dev" || strings.HasPrefix(version, "dev-") {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		return readDevelopmentRemoteCLI(filepath.Dir(exe), goos, goarch)
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	client, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{
		ResponseHeaderTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	client.Timeout = remoteCLIDownloadTimeout
	return releaseasset.DownloadCLI(ctx, client, version, goos, goarch)
}

func readDevelopmentRemoteCLI(serviceDir, goos, goarch string) ([]byte, error) {
	// Only select known cross-compilation targets; never construct a path from
	// unchecked remote platform strings.
	if (goos != "linux" && goos != "darwin") || (goarch != "amd64" && goarch != "arm64") {
		return nil, fmt.Errorf("unsupported development remote CLI target: %s/%s", goos, goarch)
	}
	path := filepath.Join(serviceDir, "remote-cli", goos+"-"+goarch, "reasonix")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("development remote CLI unavailable at %s; run node desktop/scripts/build-remote-cli.mjs %s/%s: %w", path, goos, goarch, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("development remote CLI is empty: %s", path)
	}
	return data, nil
}
