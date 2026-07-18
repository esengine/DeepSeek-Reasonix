package lifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/remote/protocol"
)

type managedManifest struct {
	Format  string           `json:"format"`
	Profile InstallProfile   `json:"profile"`
	BuildID protocol.BuildID `json:"buildId"`
	SHA256  string           `json:"sha256"`
	Size    int64            `json:"size"`
}

func (m *SystemdManager) syncManagedBinary(ctx context.Context) (IdentityStatus, error) {
	if err := ctx.Err(); err != nil {
		return IdentityStatus{}, err
	}
	if err := m.ensureManagedDirectories(); err != nil {
		return IdentityStatus{}, err
	}

	source, _, err := m.openExecutableSource()
	if err != nil {
		return IdentityStatus{}, err
	}
	defer source.Close()

	temporaryBinary, err := os.CreateTemp(m.managedDir, ".reasonix-sync-*")
	if err != nil {
		return IdentityStatus{}, fmt.Errorf("create managed binary temporary file: %w", err)
	}
	temporaryBinaryPath := temporaryBinary.Name()
	keepTemporaryBinary := false
	defer func() {
		_ = temporaryBinary.Close()
		if !keepTemporaryBinary {
			_ = os.Remove(temporaryBinaryPath)
		}
	}()
	if err := temporaryBinary.Chmod(0o700); err != nil {
		return IdentityStatus{}, fmt.Errorf("set managed binary temporary mode: %w", err)
	}

	hasher := sha256.New()
	size, err := copyWithContext(ctx, io.MultiWriter(temporaryBinary, hasher), source)
	if err != nil {
		return IdentityStatus{}, fmt.Errorf("copy current executable: %w", err)
	}
	if err := temporaryBinary.Sync(); err != nil {
		return IdentityStatus{}, fmt.Errorf("sync managed binary temporary file: %w", err)
	}
	if err := temporaryBinary.Close(); err != nil {
		return IdentityStatus{}, fmt.Errorf("close managed binary temporary file: %w", err)
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	verifiedHash, verifiedSize, err := hashRegularFile(temporaryBinaryPath)
	if err != nil {
		return IdentityStatus{}, fmt.Errorf("verify managed binary temporary file: %w", err)
	}
	if verifiedHash != hash || verifiedSize != size {
		return IdentityStatus{}, fmt.Errorf("managed binary temporary verification mismatch: %w", ErrUnsafeArtifact)
	}

	manifest := managedManifest{
		Format:  ManifestFormat,
		Profile: m.profile,
		BuildID: m.cliBuildID,
		SHA256:  hash,
		Size:    size,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return IdentityStatus{}, fmt.Errorf("encode managed binary manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	manifestTemporaryPath, err := writeTemporaryFile(m.managedDir, ".reasonix-manifest-*", manifestBytes, 0o600)
	if err != nil {
		return IdentityStatus{}, fmt.Errorf("prepare managed binary manifest: %w", err)
	}
	keepTemporaryManifest := false
	defer func() {
		if !keepTemporaryManifest {
			_ = os.Remove(manifestTemporaryPath)
		}
	}()

	if err := os.Rename(temporaryBinaryPath, m.binaryPath); err != nil {
		return IdentityStatus{}, fmt.Errorf("replace managed binary: %w", err)
	}
	keepTemporaryBinary = true
	if err := syncDirectory(m.managedDir); err != nil {
		return IdentityStatus{}, fmt.Errorf("sync managed binary directory: %w", err)
	}
	if err := os.Rename(manifestTemporaryPath, m.manifestPath); err != nil {
		return IdentityStatus{}, fmt.Errorf("replace managed binary manifest: %w", err)
	}
	keepTemporaryManifest = true
	if err := syncDirectory(m.managedDir); err != nil {
		return IdentityStatus{}, fmt.Errorf("sync managed manifest directory: %w", err)
	}

	identity, err := m.inspectInstalledIdentity()
	if err != nil {
		return identity, fmt.Errorf("post-write managed binary verification: %w", err)
	}
	if !identity.Valid || identity.BuildID == nil || protocol.CompareBuildID(m.cliBuildID, *identity.BuildID) != nil {
		return identity, fmt.Errorf("post-write managed binary identity mismatch: %w", ErrUnsafeArtifact)
	}
	return identity, nil
}

func (m *SystemdManager) ensureManagedDirectories() error {
	if err := ensureTrustedDirectory(m.profile.ReasonixHome, m.uid, true, nil); err != nil {
		return fmt.Errorf("Reasonix Home: %w", err)
	}
	if err := ensureTrustedDirectory(m.managedRoot, m.uid, true, exactMode(0o700)); err != nil {
		return fmt.Errorf("remote managed directory: %w", err)
	}
	if err := ensureTrustedDirectory(m.managedDir, m.uid, true, exactMode(0o700)); err != nil {
		return fmt.Errorf("remote managed binary directory: %w", err)
	}
	return nil
}

func (m *SystemdManager) inspectInstalledIdentity() (IdentityStatus, error) {
	binaryStatus := inspectFile(m.binaryPath, m.uid)
	manifestStatus := inspectFile(m.manifestPath, m.uid)
	identity := IdentityStatus{Present: binaryStatus.Exists || manifestStatus.Exists}
	if !binaryStatus.Exists && !manifestStatus.Exists {
		return identity, ErrNotInstalled
	}
	if !secureRegularExecutable(binaryStatus) {
		identity.Error = "managed binary is not a secure owned executable"
		return identity, fmt.Errorf("%s: %w", identity.Error, ErrUnsafeArtifact)
	}
	if !secureRegularData(manifestStatus) {
		identity.Error = "managed manifest is not a secure owned regular file"
		return identity, fmt.Errorf("%s: %w", identity.Error, ErrUnsafeArtifact)
	}

	manifestBytes, err := os.ReadFile(m.manifestPath)
	if err != nil {
		identity.Error = err.Error()
		return identity, err
	}
	var manifest managedManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		identity.Error = "invalid managed manifest: " + err.Error()
		return identity, fmt.Errorf("decode managed binary manifest: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		identity.Error = "invalid managed manifest: " + err.Error()
		return identity, fmt.Errorf("decode managed binary manifest: %w", err)
	}
	identity.Profile = &manifest.Profile
	identity.BuildID = &manifest.BuildID
	if manifest.Format != ManifestFormat {
		identity.Error = "managed manifest format mismatch"
		return identity, fmt.Errorf("%s: %w", identity.Error, ErrUnsafeArtifact)
	}
	if err := manifest.BuildID.Validate(); err != nil {
		identity.Error = "invalid managed Build ID: " + err.Error()
		return identity, fmt.Errorf("%s: %w", identity.Error, ErrUnsafeArtifact)
	}
	if !sameInstallProfile(manifest.Profile, m.profile) {
		identity.Error = "managed install profile belongs to a different Reasonix Home"
		return identity, fmt.Errorf("%s: %w", identity.Error, ErrProfileMismatch)
	}
	actualHash, actualSize, err := hashRegularFile(m.binaryPath)
	if err != nil {
		identity.Error = err.Error()
		return identity, err
	}
	identity.SHA256 = actualHash
	identity.Size = actualSize
	if !validSHA256(manifest.SHA256) || !strings.EqualFold(actualHash, manifest.SHA256) || actualSize != manifest.Size {
		identity.Error = "managed binary hash or size does not match its manifest"
		return identity, fmt.Errorf("%s: %w", identity.Error, ErrUnsafeArtifact)
	}
	identity.Valid = true
	return identity, nil
}

func inspectFile(path string, expectedUID int) FileStatus {
	status := FileStatus{Path: path}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return status
	}
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	status.Mode = uint32(info.Mode())
	status.ModeText = info.Mode().String()
	fileUID, ownerKnown := ownerUID(info)
	status.UID = int64(fileUID)
	status.OwnerKnown = ownerKnown
	status.OwnerMatches = !ownerKnown || fileUID == expectedUID
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		status.Kind = "symlink"
		status.Symlink = true
	case info.Mode().IsRegular():
		status.Kind = "regular"
	case info.IsDir():
		status.Kind = "directory"
	case info.Mode()&os.ModeSocket != 0:
		status.Kind = "socket"
	default:
		status.Kind = "other"
	}
	return status
}

func secureRegularExecutable(status FileStatus) bool {
	mode := os.FileMode(status.Mode)
	return status.Exists && status.Error == "" && status.Kind == "regular" && status.OwnerMatches && mode.Perm()&0o022 == 0 && mode.Perm()&0o100 != 0
}

func secureRegularData(status FileStatus) bool {
	mode := os.FileMode(status.Mode)
	return status.Exists && status.Error == "" && status.Kind == "regular" && status.OwnerMatches && mode.Perm()&0o022 == 0
}

func hashRegularFile(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%q is not a regular non-symlink file: %w", path, ErrUnsafeArtifact)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func writeTemporaryFile(directory, pattern string, contents []byte, mode os.FileMode) (path string, err error) {
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", err
	}
	path = file.Name()
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := file.Write(contents); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	keep = true
	return path, nil
}

func atomicWriteFile(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporaryPath, err := writeTemporaryFile(directory, ".reasonix-write-*", contents, mode)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	keep = true
	return syncDirectory(directory)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON value")
	}
	return err
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sameInstallProfile(left, right InstallProfile) bool {
	return left.ReasonixHome == right.ReasonixHome && left.ID == right.ID && left.ID == profileID(left.ReasonixHome)
}
