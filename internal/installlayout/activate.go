package installlayout

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"reasonix/internal/filelock"
)

const activationLockName = ".reasonix-activate.lock"

// Member is one file to publish into a version directory.
type Member struct {
	// Name is a base name only (no directories). Must be on the allow-list.
	Name string
	// Path is a regular file on the same volume as the install root (preferred)
	// or any readable regular file that will be copied.
	Path string
	// Mode is the destination file mode. Zero defaults to 0o755.
	Mode os.FileMode
}

// ActivationRequest describes a one-shot version publish + pointer swap.
type ActivationRequest struct {
	InstallRoot string
	Version     string
	// RequestID isolates staging directories so concurrent/failed attempts never
	// share a global pending file.
	RequestID string
	Members   []Member
	// RequiredNames, when non-empty, is the exact member whitelist. Defaults to
	// the platform desktop release unit.
	RequiredNames []string
	// RootMembers are stable entry points published at InstallRoot before the
	// current.json commit. They are rolled back if any later step fails.
	RootMembers []Member
	// RequiredRootNames is the exact root-entry whitelist when RootMembers is
	// non-empty. Callers must provide it explicitly.
	RequiredRootNames []string
}

// AllowedVersionMembers returns the default files inside versions/<version>/.
func AllowedVersionMembers() []string {
	names := []string{
		DesktopBinaryName(),
		CLIBinaryName(),
		UpdateHelperBinaryName(),
	}
	return names
}

// StagingDirName builds versions/.staging-<version>-<nonce> for one request.
func StagingDirName(version, nonce string) string {
	version = strings.TrimSpace(version)
	nonce = strings.TrimSpace(nonce)
	return fmt.Sprintf(".staging-%s-%s", version, nonce)
}

// ActivateVersion copies members into a unique staging directory on the same
// volume, validates the whitelist, renames staging to versions/<version>, and
// finally swaps current.json. Any failure before the pointer swap leaves the
// previous active version unchanged.
func ActivateVersion(req ActivationRequest) error {
	installRoot, err := cleanInstallRoot(req.InstallRoot)
	if err != nil {
		return err
	}
	if err := ValidateVersionName(req.Version); err != nil {
		return err
	}
	required := req.RequiredNames
	if len(required) == 0 {
		required = AllowedVersionMembers()
	}
	if err := validateMembers(req.Members, required); err != nil {
		return err
	}
	if len(req.RootMembers) > 0 {
		if len(req.RequiredRootNames) == 0 {
			return fmt.Errorf("installlayout: root member whitelist is required")
		}
		if err := validateMembers(req.RootMembers, req.RequiredRootNames); err != nil {
			return fmt.Errorf("installlayout: root entries: %w", err)
		}
	} else if len(req.RequiredRootNames) > 0 {
		return fmt.Errorf("installlayout: root member whitelist provided without root members")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	unlock, err := filelock.Acquire(ctx, filepath.Join(installRoot, activationLockName))
	if err != nil {
		return fmt.Errorf("installlayout: acquire activation lock: %w", err)
	}
	defer unlock()

	versionsRoot := filepath.Join(installRoot, VersionsDirName)
	if err := os.MkdirAll(versionsRoot, 0o755); err != nil {
		return fmt.Errorf("installlayout: create versions dir: %w", err)
	}
	if err := rejectSymlinkPathComponents(installRoot, VersionsDirName); err != nil {
		return err
	}

	nonce, err := stagingNonce(req.RequestID)
	if err != nil {
		return err
	}
	stagingName := StagingDirName(req.Version, nonce)
	stagingPath := filepath.Join(versionsRoot, stagingName)
	rootStagingPath := filepath.Join(versionsRoot, ".root-"+stagingName)
	// Always start clean for this request id/nonce.
	_ = os.RemoveAll(stagingPath)
	_ = os.RemoveAll(rootStagingPath)
	if err := os.Mkdir(stagingPath, 0o755); err != nil {
		return fmt.Errorf("installlayout: create staging dir: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stagingPath)
		}
		_ = os.RemoveAll(rootStagingPath)
	}()

	for _, m := range req.Members {
		if err := publishMember(stagingPath, m); err != nil {
			return err
		}
	}
	// Ensure every required name exists as a regular file (no symlinks).
	for _, name := range required {
		path := filepath.Join(stagingPath, name)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("installlayout: staged member %s: %w", name, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("installlayout: staged member %s is not a regular file", name)
		}
	}
	if len(req.RootMembers) > 0 {
		if err := os.Mkdir(rootStagingPath, 0o755); err != nil {
			return fmt.Errorf("installlayout: create root staging dir: %w", err)
		}
		for _, m := range req.RootMembers {
			if err := publishMember(rootStagingPath, m); err != nil {
				return fmt.Errorf("installlayout: stage root entry: %w", err)
			}
		}
	}

	finalRel := VersionDirRelative(req.Version)
	finalPath := filepath.Join(installRoot, filepath.FromSlash(finalRel))
	var versionBackup string
	if _, err := os.Lstat(finalPath); err == nil {
		// A previous partial publish of the same version is replaced only from
		// staging after validation. Never swap current.json first.
		versionBackup = finalPath + ".replaced-" + nonce
		_ = os.RemoveAll(versionBackup)
		if err := os.Rename(finalPath, versionBackup); err != nil {
			return fmt.Errorf("installlayout: displace existing version dir: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("installlayout: inspect version dir: %w", err)
	}

	if err := os.Rename(stagingPath, finalPath); err != nil {
		if versionBackup != "" {
			_ = os.Rename(versionBackup, finalPath)
		}
		return fmt.Errorf("installlayout: publish version directory: %w", err)
	}
	rollbackVersion := func() error {
		var rollbackErr error
		if err := os.RemoveAll(finalPath); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
		if versionBackup != "" {
			if err := os.Rename(versionBackup, finalPath); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		return rollbackErr
	}

	rollbackRoots, commitRoots, err := publishRootEntries(installRoot, rootStagingPath, req.RootMembers)
	if err != nil {
		if rollbackErr := rollbackVersion(); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("installlayout: restore version after root publish failure: %w", rollbackErr))
		}
		return err
	}

	ptr := CurrentPointer{
		SchemaVersion: CurrentSchemaVersion,
		ActiveVersion: req.Version,
		ActiveDir:     finalRel,
	}
	if err := WriteCurrent(installRoot, ptr); err != nil {
		rootErr := rollbackRoots()
		versionErr := rollbackVersion()
		return errors.Join(
			fmt.Errorf("installlayout: write current.json: %w", err),
			wrapRollbackError("restore root entries", rootErr),
			wrapRollbackError("restore version directory", versionErr),
		)
	}
	committed = true
	commitRoots()
	if versionBackup != "" {
		_ = os.RemoveAll(versionBackup)
	}
	return nil
}

func publishRootEntries(installRoot, stagingRoot string, members []Member) (rollback func() error, commit func(), err error) {
	if len(members) == 0 {
		return func() error { return nil }, func() {}, nil
	}
	backupRoot := filepath.Join(stagingRoot, ".backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return nil, nil, fmt.Errorf("installlayout: create root backup staging: %w", err)
	}
	type replacement struct {
		destination string
		backup      string
		hadOriginal bool
	}
	replacements := make([]replacement, 0, len(members))
	rollbackFn := func() error {
		var rollbackErr error
		for _, v := range slices.Backward(replacements) {
			r := v
			if err := os.Remove(r.destination); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
			if r.hadOriginal {
				if err := os.Rename(r.backup, r.destination); err != nil {
					rollbackErr = errors.Join(rollbackErr, err)
				}
			}
		}
		return rollbackErr
	}
	for _, m := range members {
		name := filepath.Base(m.Name)
		source := filepath.Join(stagingRoot, name)
		destination := filepath.Join(installRoot, name)
		r := replacement{destination: destination, backup: filepath.Join(backupRoot, name)}
		if info, statErr := os.Lstat(destination); statErr == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				_ = rollbackFn()
				return nil, nil, fmt.Errorf("installlayout: root entry %s is not a regular file", name)
			}
			if err := os.Rename(destination, r.backup); err != nil {
				_ = rollbackFn()
				return nil, nil, fmt.Errorf("installlayout: back up root entry %s: %w", name, err)
			}
			r.hadOriginal = true
		} else if !os.IsNotExist(statErr) {
			_ = rollbackFn()
			return nil, nil, fmt.Errorf("installlayout: inspect root entry %s: %w", name, statErr)
		}
		replacements = append(replacements, r)
		if err := os.Rename(source, destination); err != nil {
			rollbackErr := rollbackFn()
			return nil, nil, errors.Join(
				fmt.Errorf("installlayout: publish root entry %s: %w", name, err),
				wrapRollbackError("restore root entries", rollbackErr),
			)
		}
	}
	return rollbackFn, func() { _ = os.RemoveAll(backupRoot) }, nil
}

func wrapRollbackError(label string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("installlayout: %s: %w", label, err)
}

func validateMembers(members []Member, required []string) error {
	if len(members) == 0 {
		return fmt.Errorf("installlayout: no members to activate")
	}
	allowed := make(map[string]struct{}, len(required))
	for _, name := range required {
		allowed[normalizeMemberName(name)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(members))
	for _, m := range members {
		name := normalizeMemberName(m.Name)
		if name == "" || name != filepath.Base(name) || strings.Contains(name, `\`) {
			return fmt.Errorf("installlayout: member name %q is invalid", m.Name)
		}
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("installlayout: member %q is not allowed", m.Name)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("installlayout: duplicate member %q", m.Name)
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(m.Path) == "" {
			return fmt.Errorf("installlayout: member %q path is empty", m.Name)
		}
	}
	for _, name := range required {
		if _, ok := seen[normalizeMemberName(name)]; !ok {
			return fmt.Errorf("installlayout: required member %q is missing", name)
		}
	}
	if len(seen) != len(required) {
		return fmt.Errorf("installlayout: member set does not match required whitelist")
	}
	return nil
}

func normalizeMemberName(name string) string {
	name = strings.TrimSpace(name)
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

func publishMember(stagingDir string, m Member) error {
	src := filepath.Clean(strings.TrimSpace(m.Path))
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("installlayout: source %s: %w", m.Name, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("installlayout: source %s is not a regular file", m.Name)
	}
	mode := m.Mode
	if mode == 0 {
		mode = 0o755
	}
	dst := filepath.Join(stagingDir, filepath.Base(m.Name))
	if err := copyFileRegular(src, dst, mode); err != nil {
		return fmt.Errorf("installlayout: copy %s: %w", m.Name, err)
	}
	return nil
}

func copyFileRegular(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func stagingNonce(requestID string) (string, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID != "" {
		// Sanitize request id into a short filesystem-safe token.
		var b strings.Builder
		for _, r := range requestID {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				b.WriteRune(r)
			}
			if b.Len() >= 24 {
				break
			}
		}
		if b.Len() >= 6 {
			return b.String(), nil
		}
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// Fall back to time-based uniqueness if the platform CSPRNG fails.
		return fmt.Sprintf("%d", time.Now().UnixNano()), nil
	}
	return hex.EncodeToString(raw[:]), nil
}

// CleanupStaleStaging removes versions/.staging-* directories older than maxAge.
// Safe to call anytime; never touches published version directories or current.json.
func CleanupStaleStaging(installRoot string, maxAge time.Duration) error {
	installRoot, err := cleanInstallRoot(installRoot)
	if err != nil {
		return err
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	versionsRoot := filepath.Join(installRoot, VersionsDirName)
	entries, err := os.ReadDir(versionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".staging-") {
			continue
		}
		path := filepath.Join(versionsRoot, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(path)
	}
	return nil
}

// RetainPreviousVersions keeps the active version plus at most one previous
// version directory for signed recovery installers. Older trees are removed.
// The launcher never auto-selects a previous version; retention is for manual
// recovery packages only.
func RetainPreviousVersions(installRoot string, keep time.Duration) error {
	installRoot, err := cleanInstallRoot(installRoot)
	if err != nil {
		return err
	}
	ptr, err := ReadCurrent(installRoot)
	if err != nil {
		return err
	}
	if keep <= 0 {
		keep = 7 * 24 * time.Hour
	}
	versionsRoot := filepath.Join(installRoot, VersionsDirName)
	entries, err := os.ReadDir(versionsRoot)
	if err != nil {
		return err
	}
	type verDir struct {
		name string
		mod  time.Time
	}
	var previous []verDir
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if name == ptr.ActiveVersion {
			continue
		}
		if err := ValidateVersionName(name); err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		previous = append(previous, verDir{name: name, mod: info.ModTime()})
	}
	// Keep the newest previous version if it is within the retention window;
	// delete everything else.
	var newest *verDir
	for i := range previous {
		p := &previous[i]
		if newest == nil || p.mod.After(newest.mod) {
			newest = p
		}
	}
	cutoff := time.Now().Add(-keep)
	for _, p := range previous {
		if newest != nil && p.name == newest.name && !p.mod.Before(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(versionsRoot, p.name))
	}
	return nil
}
