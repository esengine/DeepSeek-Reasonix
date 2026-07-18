package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/buildinfo"
	"reasonix/internal/config"
	"reasonix/internal/remote/protocol"
)

const remoteHostsFileName = "remote-hosts.json"

// currentDesktopRemoteBuildID derives the Desktop peer identity from the same
// product version and source-revision inputs used by official CLI/Desktop build
// entry points. It deliberately has no development fallback: a binary without
// an exact VCS identity cannot claim compatibility with an attach CLI/daemon.
func currentDesktopRemoteBuildID() (protocol.BuildID, error) {
	id, err := protocol.NewBuildID(version, buildinfo.Revision())
	if err != nil {
		return protocol.BuildID{}, fmt.Errorf("Desktop Remote Build ID: %w", err)
	}
	return id, nil
}

func defaultRemoteHostStorePath() (string, error) {
	dir := strings.TrimSpace(config.MemoryUserDir())
	if dir == "" {
		return "", fmt.Errorf("Desktop user state directory is unavailable")
	}
	return filepath.Join(dir, remoteHostsFileName), nil
}
