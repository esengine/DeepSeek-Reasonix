// Package installlayout implements the Reasonix v1.20+ versioned install layout:
// InstallRoot/{current.json, reasonix-launcher, versions/<version>/...}.
//
// The desktop launcher only reads current.json and starts the active desktop
// binary. It never counts crashes, chooses previous versions, or enters a
// product "safe mode". Update activation stages under versions/.staging-* and
// only swaps current.json after the version directory is fully published.
package installlayout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	"reasonix/internal/fileutil"
)

const (
	// CurrentSchemaVersion is the only accepted current.json schema.
	CurrentSchemaVersion = 1
	// CurrentFileName is the active-version pointer under InstallRoot.
	CurrentFileName = "current.json"
	// VersionsDirName holds published version trees and staging directories.
	VersionsDirName = "versions"
	// InstallLayoutVersionedV1 is the manifest Asset.install_layout value for
	// this layout. Unknown layouts must be rejected by the new client.
	InstallLayoutVersionedV1 = "versioned-v1"
)

// versionDirRE accepts published version directory names such as v1.20.0 or
// v1.20.0-preview.1. The directory name is also the activeVersion string.
var versionDirRE = regexp.MustCompile(`^v[0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z.-]+)?$`)

// CurrentPointer is the on-disk content of current.json (schema 1).
type CurrentPointer struct {
	SchemaVersion int    `json:"schemaVersion"`
	ActiveVersion string `json:"activeVersion"`
	// ActiveDir is a relative path under InstallRoot, constrained to
	// versions/<version> with no absolute path, ".." segments, or symlink hops.
	ActiveDir string `json:"activeDir"`
}

// ReadCurrent loads and validates current.json under installRoot.
func ReadCurrent(installRoot string) (CurrentPointer, error) {
	installRoot, err := cleanInstallRoot(installRoot)
	if err != nil {
		return CurrentPointer{}, err
	}
	path := filepath.Join(installRoot, CurrentFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return CurrentPointer{}, err
	}
	ptr, err := DecodeCurrent(data)
	if err != nil {
		return CurrentPointer{}, err
	}
	if err := ValidateActiveDir(installRoot, ptr.ActiveVersion, ptr.ActiveDir); err != nil {
		return CurrentPointer{}, err
	}
	return ptr, nil
}

// DecodeCurrent parses a current.json payload without checking the install tree.
func DecodeCurrent(data []byte) (CurrentPointer, error) {
	var ptr CurrentPointer
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ptr); err != nil {
		return CurrentPointer{}, fmt.Errorf("installlayout: decode current.json: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return CurrentPointer{}, fmt.Errorf("installlayout: decode current.json: trailing JSON value")
		}
		return CurrentPointer{}, fmt.Errorf("installlayout: decode current.json: %w", err)
	}
	if ptr.SchemaVersion != CurrentSchemaVersion {
		return CurrentPointer{}, fmt.Errorf("installlayout: current.json schema %d is unsupported", ptr.SchemaVersion)
	}
	if err := ValidateVersionName(ptr.ActiveVersion); err != nil {
		return CurrentPointer{}, err
	}
	if err := ValidateActiveDirRelative(ptr.ActiveVersion, ptr.ActiveDir); err != nil {
		return CurrentPointer{}, err
	}
	return ptr, nil
}

// WriteCurrent atomically replaces current.json. Call only after the version
// directory is fully published; failures leave the previous pointer intact when
// the OS supports atomic rename of an existing file.
func WriteCurrent(installRoot string, ptr CurrentPointer) error {
	installRoot, err := cleanInstallRoot(installRoot)
	if err != nil {
		return err
	}
	if ptr.SchemaVersion == 0 {
		ptr.SchemaVersion = CurrentSchemaVersion
	}
	if ptr.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("installlayout: current.json schema %d is unsupported", ptr.SchemaVersion)
	}
	if err := ValidateVersionName(ptr.ActiveVersion); err != nil {
		return err
	}
	if strings.TrimSpace(ptr.ActiveDir) == "" {
		ptr.ActiveDir = VersionDirRelative(ptr.ActiveVersion)
	}
	if err := ValidateActiveDir(installRoot, ptr.ActiveVersion, ptr.ActiveDir); err != nil {
		return err
	}
	body, err := json.MarshalIndent(ptr, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	// current.json is the layout commit point. Never use AtomicWriteFile's
	// cross-device copy fallback here: truncating this file can make every
	// installed version unreachable through the launcher.
	return fileutil.AtomicWriteFileStrict(filepath.Join(installRoot, CurrentFileName), body, 0o644)
}

// ValidateVersionName rejects empty, absolute, or traversal-prone version labels.
func ValidateVersionName(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("installlayout: activeVersion is empty")
	}
	if strings.Contains(version, `\`) || strings.Contains(version, "/") || strings.Contains(version, "..") {
		return fmt.Errorf("installlayout: activeVersion %q is invalid", version)
	}
	if !versionDirRE.MatchString(version) {
		return fmt.Errorf("installlayout: activeVersion %q is invalid", version)
	}
	for _, r := range version {
		if r > unicode.MaxASCII || (!unicode.IsPrint(r)) {
			return fmt.Errorf("installlayout: activeVersion %q is invalid", version)
		}
	}
	return nil
}

// VersionDirRelative returns versions/<version> using forward slashes for the
// JSON field (normalized later with filepath on disk).
func VersionDirRelative(version string) string {
	return VersionsDirName + "/" + version
}

// ValidateActiveDirRelative checks the activeDir field shape only.
func ValidateActiveDirRelative(version, activeDir string) error {
	if err := ValidateVersionName(version); err != nil {
		return err
	}
	activeDir = strings.TrimSpace(activeDir)
	if activeDir == "" {
		return fmt.Errorf("installlayout: activeDir is empty")
	}
	if filepath.IsAbs(activeDir) {
		return fmt.Errorf("installlayout: activeDir must be relative")
	}
	slash := filepath.ToSlash(activeDir)
	if strings.HasPrefix(slash, "/") || strings.HasPrefix(slash, "../") || strings.Contains(slash, "/../") || strings.HasSuffix(slash, "/..") || slash == ".." {
		return fmt.Errorf("installlayout: activeDir must not contain path traversal")
	}
	want := VersionDirRelative(version)
	if slash != want {
		return fmt.Errorf("installlayout: activeDir %q must equal %q", slash, want)
	}
	return nil
}

// ValidateActiveDir ensures activeDir resolves under installRoot/versions/<version>
// without following a symlink at the version directory itself.
func ValidateActiveDir(installRoot, version, activeDir string) error {
	installRoot, err := cleanInstallRoot(installRoot)
	if err != nil {
		return err
	}
	if err := ValidateActiveDirRelative(version, activeDir); err != nil {
		return err
	}
	abs := filepath.Join(installRoot, filepath.FromSlash(filepath.ToSlash(activeDir)))
	rel, err := filepath.Rel(installRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("installlayout: activeDir escapes install root")
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("installlayout: active version directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("installlayout: active version path is not a real directory")
	}
	// Reject a symlink at any path component under install root.
	if err := rejectSymlinkPathComponents(installRoot, rel); err != nil {
		return err
	}
	return nil
}

func rejectSymlinkPathComponents(root, rel string) error {
	cur := root
	for part := range strings.SplitSeq(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return fmt.Errorf("installlayout: inspect %s: %w", part, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("installlayout: symlink component %q is not allowed", part)
		}
	}
	return nil
}

func cleanInstallRoot(installRoot string) (string, error) {
	installRoot = filepath.Clean(strings.TrimSpace(installRoot))
	if installRoot == "" || installRoot == "." {
		return "", fmt.Errorf("installlayout: install root is empty")
	}
	if !filepath.IsAbs(installRoot) {
		return "", fmt.Errorf("installlayout: install root must be absolute")
	}
	info, err := os.Lstat(installRoot)
	if err != nil {
		return "", fmt.Errorf("installlayout: install root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("installlayout: install root must be a real directory")
	}
	return installRoot, nil
}

// DesktopBinaryName is the platform-specific desktop executable base name.
func DesktopBinaryName() string {
	if runtime.GOOS == "windows" {
		return "reasonix-desktop.exe"
	}
	return "reasonix-desktop"
}

// CLIBinaryName is the platform-specific CLI executable base name inside a
// version directory.
func CLIBinaryName() string {
	if runtime.GOOS == "windows" {
		return "reasonix-cli.exe"
	}
	return "reasonix-cli"
}

// UpdateHelperBinaryName is the platform-specific update helper name.
func UpdateHelperBinaryName() string {
	if runtime.GOOS == "windows" {
		return "reasonix-update-helper.exe"
	}
	return "reasonix-update-helper"
}

// ActiveDesktopPath resolves the active desktop executable from current.json.
func ActiveDesktopPath(installRoot string) (string, error) {
	ptr, err := ReadCurrent(installRoot)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(installRoot, filepath.FromSlash(ptr.ActiveDir))
	path := filepath.Join(dir, DesktopBinaryName())
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("installlayout: active desktop binary: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("installlayout: active desktop binary is not a regular file")
	}
	return path, nil
}

// ActiveCLIPath resolves the active CLI executable from current.json.
func ActiveCLIPath(installRoot string) (string, error) {
	ptr, err := ReadCurrent(installRoot)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(installRoot, filepath.FromSlash(ptr.ActiveDir))
	path := filepath.Join(dir, CLIBinaryName())
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("installlayout: active CLI binary: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("installlayout: active CLI binary is not a regular file")
	}
	return path, nil
}

// HasCurrent reports whether installRoot already uses the versioned layout.
func HasCurrent(installRoot string) bool {
	_, err := ReadCurrent(installRoot)
	return err == nil
}

// ResolveInstallRoot walks upward from path (usually the running executable)
// and returns the InstallRoot that owns current.json. Flat installs return the
// directory containing the executable when no pointer is found.
func ResolveInstallRoot(fromPath string) (string, error) {
	fromPath = filepath.Clean(strings.TrimSpace(fromPath))
	if fromPath == "" {
		return "", fmt.Errorf("installlayout: empty path")
	}
	info, err := os.Lstat(fromPath)
	if err != nil {
		return "", err
	}
	dir := fromPath
	if !info.IsDir() {
		dir = filepath.Dir(fromPath)
	}
	cur := dir
	for {
		if HasCurrent(cur) {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// No versioned layout found: treat the original directory as the
			// flat install root.
			return dir, nil
		}
		// Stop climbing out of a versions tree once we pass InstallRoot.
		base := filepath.Base(cur)
		if base == VersionsDirName {
			// parent is InstallRoot even without current.json yet (migration).
			return parent, nil
		}
		cur = parent
	}
}

// ActiveUpdateHelperPath resolves the active update helper binary.
func ActiveUpdateHelperPath(installRoot string) (string, error) {
	ptr, err := ReadCurrent(installRoot)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(installRoot, filepath.FromSlash(ptr.ActiveDir))
	path := filepath.Join(dir, UpdateHelperBinaryName())
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("installlayout: active update helper: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("installlayout: active update helper is not a regular file")
	}
	return path, nil
}

// LauncherBinaryName is the permanent thin launcher at InstallRoot.
func LauncherBinaryName() string {
	if runtime.GOOS == "windows" {
		return "reasonix-launcher.exe"
	}
	return "reasonix-launcher"
}

// PortableAliasName is the Windows portable entry (Reasonix.exe).
func PortableAliasName() string {
	if runtime.GOOS == "windows" {
		return "Reasonix.exe"
	}
	return ""
}
