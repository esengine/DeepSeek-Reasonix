package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/installlayout"
)

// stageLinuxShellRelease expands only the signed release unit. The bounded
// extraction rejects aliases, links and duplicate paths before activation;
// executable permissions survive, while setuid/setgid never enter a user tree.
func stageLinuxShellRelease(archive []byte, staging string) ([]installlayout.Member, []string, error) {
	root, err := os.OpenRoot(staging)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var expanded int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
			if name == "app" {
				continue
			}
			if err := installlayout.ValidateMemberName(name); err != nil || !strings.HasPrefix(name, "app/") {
				return nil, nil, fmt.Errorf("invalid release directory %q", h.Name)
			}
			continue
		}
		if err := installlayout.ValidateMemberName(name); err != nil {
			return nil, nil, err
		}
		switch name {
		case "reasonix-desktop", "reasonix", "reasonix-launcher", "reasonix-guard":
		default:
			if !strings.HasPrefix(name, "app/") {
				return nil, nil, fmt.Errorf("unexpected release member %q", name)
			}
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || seen[name] {
			return nil, nil, fmt.Errorf("invalid or duplicate release member %q", name)
		}
		if h.Size > (2<<30)-expanded || len(seen) >= 10000 {
			return nil, nil, fmt.Errorf("release tree exceeds extraction limit")
		}
		expanded += h.Size
		seen[name] = true
		// Resolve relative to the opened directory handle. Name validation
		// enforces the release layout; Root also prevents an existing or
		// concurrently replaced parent symlink from escaping staging.
		dst := filepath.FromSlash(name)
		if err := root.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return nil, nil, err
		}
		mode := os.FileMode(h.Mode) & 0755
		file, err := root.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return nil, nil, err
		}
		_, copyErr := io.CopyN(file, tr, h.Size)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return nil, nil, err
		}
	}
	for _, name := range []string{"reasonix-desktop", "reasonix", "reasonix-launcher"} {
		if !seen[name] {
			return nil, nil, fmt.Errorf("release missing %s", name)
		}
	}
	members, err := installlayout.ShellMembers(staging, "linux")
	if err != nil {
		return nil, nil, err
	}
	if len(members) == 0 {
		return nil, nil, fmt.Errorf("release missing Electron shell; install the complete package manually")
	}
	for _, name := range []string{"reasonix-desktop", "reasonix"} {
		members = append(members, installlayout.Member{Name: name, Path: filepath.Join(staging, name), Mode: 0755})
	}
	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, m.Name)
	}
	return members, names, nil
}

func activateLinuxShellRelease(archive []byte, targetVersion, root string) error {
	if _, err := installlayout.ReadCurrent(root); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("resolve Linux layout: %w", err)
	}
	targetVersion = strings.TrimSpace(targetVersion)
	if !strings.HasPrefix(targetVersion, "v") {
		targetVersion = "v" + targetVersion
	}
	if err := installlayout.ValidateVersionName(targetVersion); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(root, ".reasonix-linux-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	members, names, err := stageLinuxShellRelease(archive, staging)
	if err != nil {
		return err
	}
	err = installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot: root, Version: targetVersion, RequestID: "linux-" + targetVersion, Members: members, RequiredNames: names,
		RootMembers:       []installlayout.Member{{Name: "reasonix-launcher", Path: filepath.Join(staging, "reasonix-launcher"), Mode: 0755}, {Name: "reasonix", Path: filepath.Join(staging, "reasonix"), Mode: 0755}},
		RequiredRootNames: []string{"reasonix-launcher", "reasonix"},
	})
	if err != nil {
		return fmt.Errorf("activate Linux release: %w", err)
	}
	_ = installlayout.RetainPreviousVersions(root, 0)
	return nil
}
