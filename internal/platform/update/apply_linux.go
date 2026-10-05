//go:build linux

package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/installlayout"
)

// apply publishes the tarball into a new version directory and swaps the
// pointer last. Unlike Windows, Linux can replace files under a running
// process, so the install completes here and the caller relaunches.
func (v VersionedInstaller) apply(_ context.Context, c Cached) error {
	targz, err := os.ReadFile(c.Path)
	if err != nil {
		return err
	}
	release, err := ExtractReleaseUnit(targz, v.Line)
	if err != nil {
		return err
	}
	root := v.Layout.Root
	if _, err := installlayout.ReadCurrent(root); err != nil {
		return fmt.Errorf("update: resolve active Linux layout: %w", err)
	}
	target := strings.TrimSpace(c.Version)
	if !strings.HasPrefix(target, "v") {
		target = "v" + target
	}
	if err := installlayout.ValidateVersionName(target); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(root, ".reasonix-linux-update-*")
	if err != nil {
		return fmt.Errorf("update: create Linux version staging: %w", err)
	}
	defer os.RemoveAll(staging)
	members := make([]installlayout.Member, 0, len(v.Line.Members))
	for _, m := range v.Line.Members {
		if m.Installed == "" {
			continue
		}
		path := filepath.Join(staging, m.Installed)
		if err := os.WriteFile(path, release[m.Archive], m.Mode); err != nil {
			return fmt.Errorf("update: stage %s: %w", m.Installed, err)
		}
		members = append(members, installlayout.Member{Name: m.Installed, Path: path, Mode: m.Mode})
	}
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot:   root,
		Version:       target,
		RequestID:     "linux-" + target,
		Members:       members,
		RequiredNames: v.Line.InstalledNames(),
	}); err != nil {
		return fmt.Errorf("update: activate Linux version: %w", err)
	}
	_ = installlayout.RetainPreviousVersions(root, 0)
	return nil
}
