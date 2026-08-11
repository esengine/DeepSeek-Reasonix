package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/desktop/internal/update"
	"reasonix/internal/repair"
)

const maxWindowsPayloadMetadataSize = 64 << 10

var verifyWindowsPayloadManifestFn = update.Verify

type stagedFileUpdateMember struct {
	targetPath string
	content    []byte
	mode       os.FileMode
}

// loadWindowsStagedReleaseUnit validates and reads the complete NSIS payload
// before any live release-unit member is moved. An existing Reasonix.exe is the
// portable alias of reasonix-launcher.exe and reuses those staged bytes; an
// installed package that did not have the alias remains unchanged.
func loadWindowsStagedReleaseUnit(claimed *repair.UpdateTransaction, stagingDir string) ([]stagedFileUpdateMember, error) {
	if claimed == nil || claimed.TargetKind != "file" || len(claimed.Files) == 0 {
		return nil, fmt.Errorf("load staged release unit: transaction identity is incomplete")
	}
	if err := validateWindowsClaimedReleaseUnit(claimed); err != nil {
		return nil, fmt.Errorf("load staged release unit: %w", err)
	}
	stagingDir = filepath.Clean(strings.TrimSpace(stagingDir))
	if stagingDir == "" || stagingDir == "." || !filepath.IsAbs(stagingDir) {
		return nil, fmt.Errorf("load staged release unit: staging directory is invalid")
	}
	info, err := os.Lstat(stagingDir)
	if err != nil {
		return nil, fmt.Errorf("load staged release unit: inspect staging directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("load staged release unit: staging path is not a directory")
	}
	hashes, err := loadWindowsPayloadManifest(stagingDir, claimed.ToVersion)
	if err != nil {
		return nil, fmt.Errorf("load staged release unit: %w", err)
	}

	contents := make(map[string][]byte)
	members := make([]stagedFileUpdateMember, 0, len(claimed.Files))
	seenTargets := make(map[string]struct{}, len(claimed.Files))
	for _, file := range claimed.Files {
		targetPath := filepath.Clean(strings.TrimSpace(file.TargetPath))
		targetKey := strings.ToLower(targetPath)
		if targetPath == "" || targetPath == "." {
			return nil, fmt.Errorf("load staged release unit: target path is invalid")
		}
		if _, ok := seenTargets[targetKey]; ok {
			return nil, fmt.Errorf("load staged release unit: duplicate target %s", filepath.Base(targetPath))
		}
		seenTargets[targetKey] = struct{}{}
		if strings.EqualFold(filepath.Base(targetPath), "Reasonix.exe") && file.MissingBefore {
			continue
		}

		sourceName, err := windowsStagedSourceName(filepath.Base(targetPath))
		if err != nil {
			return nil, err
		}
		sourcePath := filepath.Join(stagingDir, sourceName)
		content, ok := contents[sourceName]
		if !ok {
			sourceInfo, statErr := os.Lstat(sourcePath)
			if statErr != nil {
				return nil, fmt.Errorf("load staged release unit: inspect %s: %w", sourceName, statErr)
			}
			if !sourceInfo.Mode().IsRegular() {
				return nil, fmt.Errorf("load staged release unit: %s is not a regular file", sourceName)
			}
			content, err = readVerifiedWindowsStagedPayloadFn(sourcePath)
			if err != nil {
				return nil, fmt.Errorf("load staged release unit: read %s: %w", sourceName, err)
			}
			if !strings.EqualFold(update.WindowsPayloadSHA256(content), hashes[sourceName]) {
				return nil, fmt.Errorf("load staged release unit: %s does not match the signed release manifest", sourceName)
			}
			contents[sourceName] = content
		}
		members = append(members, stagedFileUpdateMember{
			targetPath: targetPath,
			content:    content,
			mode:       0o700,
		})
	}

	// Publish the running desktop last. If an earlier member fails, the old
	// desktop remains the executable entry point that can report/retry recovery.
	sort.SliceStable(members, func(i, j int) bool {
		iPrimary := strings.EqualFold(members[i].targetPath, claimed.TargetPath)
		jPrimary := strings.EqualFold(members[j].targetPath, claimed.TargetPath)
		return !iPrimary && jPrimary
	})
	return members, nil
}

func validateWindowsClaimedReleaseUnit(claimed *repair.UpdateTransaction) error {
	if claimed == nil ||
		!strings.EqualFold(filepath.Base(claimed.TargetPath), "reasonix-desktop.exe") {
		return fmt.Errorf("claimed release unit primary executable is invalid")
	}
	required := map[string]bool{
		"reasonix-desktop.exe":       false,
		"reasonix-guard.exe":         false,
		"reasonix-launcher.exe":      false,
		"reasonix-update-helper.exe": false,
		"reasonix-cli.exe":           false,
		"reasonix.exe":               false,
	}
	installDir := filepath.Clean(filepath.Dir(claimed.TargetPath))
	for _, file := range claimed.Files {
		target := filepath.Clean(strings.TrimSpace(file.TargetPath))
		if target == "" || target == "." ||
			!strings.EqualFold(filepath.Dir(target), installDir) {
			return fmt.Errorf("claimed release unit target is outside the installation directory")
		}
		name := strings.ToLower(filepath.Base(target))
		seen, ok := required[name]
		if !ok {
			return fmt.Errorf("claimed release unit contains an unexpected target")
		}
		if seen {
			return fmt.Errorf("claimed release unit contains a duplicate target")
		}
		required[name] = true
	}
	for name, seen := range required {
		if !seen {
			return fmt.Errorf("claimed release unit omits %s", name)
		}
	}
	if len(claimed.Files) != len(required) {
		return fmt.Errorf("claimed release unit contains an unexpected target")
	}
	return nil
}

func loadWindowsPayloadManifest(stagingDir, expectedVersion string) (map[string]string, error) {
	manifest, err := readWindowsPayloadMetadata(filepath.Join(stagingDir, update.WindowsPayloadManifestName))
	if err != nil {
		return nil, fmt.Errorf("read signed release manifest: %w", err)
	}
	signature, err := readWindowsPayloadMetadata(filepath.Join(stagingDir, update.WindowsPayloadSignatureName))
	if err != nil {
		return nil, fmt.Errorf("read signed release manifest signature: %w", err)
	}
	if err := verifyWindowsPayloadManifestFn(manifest, signature); err != nil {
		return nil, fmt.Errorf("verify signed release manifest: %w", err)
	}
	hashes, err := update.DecodeWindowsPayloadManifest(manifest, expectedVersion)
	if err != nil {
		return nil, err
	}
	return hashes, nil
}

func readWindowsPayloadMetadata(path string) ([]byte, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxWindowsPayloadMetadataSize {
		return nil, fmt.Errorf("%s is not a bounded regular file", filepath.Base(path))
	}
	if !os.SameFile(pathInfo, info) {
		return nil, fmt.Errorf("%s changed before it was opened", filepath.Base(path))
	}
	data, err := io.ReadAll(io.LimitReader(file, maxWindowsPayloadMetadataSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxWindowsPayloadMetadataSize {
		return nil, fmt.Errorf("%s changed size while it was read", filepath.Base(path))
	}
	return data, nil
}

func windowsStagedSourceName(targetBase string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(targetBase)) {
	case "reasonix-desktop.exe":
		return "reasonix-desktop.exe", nil
	case "reasonix-guard.exe":
		return "reasonix-guard.exe", nil
	case "reasonix-launcher.exe":
		return "reasonix-launcher.exe", nil
	case "reasonix-update-helper.exe":
		return "reasonix-update-helper.exe", nil
	case "reasonix-cli.exe":
		return "reasonix-cli.exe", nil
	case "reasonix.exe":
		return "reasonix-launcher.exe", nil
	default:
		return "", fmt.Errorf("load staged release unit: unsupported target %q", targetBase)
	}
}

func publishLoadedFileUpdateReleaseUnit(
	claimed *repair.UpdateTransaction,
	members []stagedFileUpdateMember,
	publish func(*repair.UpdateTransaction, string, []byte, os.FileMode) (repair.FileUpdateInstallReceipt, error),
) ([]repair.FileUpdateInstallReceipt, error) {
	if publish == nil || len(members) == 0 {
		return nil, fmt.Errorf("publish staged release unit: payload is incomplete")
	}
	receipts := make([]repair.FileUpdateInstallReceipt, 0, len(members))
	for _, member := range members {
		receipt, err := publish(claimed, member.targetPath, member.content, member.mode)
		if err != nil {
			return receipts, fmt.Errorf("publish staged release unit %s: %w", filepath.Base(member.targetPath), err)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}
