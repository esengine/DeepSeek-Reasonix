package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"reasonix/internal/installlayout"
)

const (
	// WindowsPayloadManifestSchemaVersion lists the flat release unit plus every
	// file of the app/ shell tree; schema 1 manifests carry the flat list only.
	WindowsPayloadManifestSchemaVersion = 2
	windowsPayloadFlatSchemaVersion     = 1
	WindowsPayloadManifestName          = "reasonix-payload.json"
	WindowsPayloadSignatureName         = WindowsPayloadManifestName + ".minisig"
	// WindowsPayloadTreePrefix starts every shell tree entry name.
	WindowsPayloadTreePrefix = installlayout.AppShellDirName + "/"
)

var windowsPayloadFileNames = [...]string{
	"reasonix-desktop.exe",
	"reasonix-guard.exe",
	"reasonix-launcher.exe",
	"reasonix-update-helper.exe",
	"reasonix-cli.exe",
}

var windowsPayloadVersionFileNames = [...]string{
	"reasonix-desktop.exe",
	"reasonix-update-helper.exe",
	"reasonix-cli.exe",
}

type WindowsPayloadManifest struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Version       string                       `json:"version"`
	Files         []WindowsPayloadManifestFile `json:"files"`
}

type WindowsPayloadManifestFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func WindowsPayloadFileNames() []string {
	return append([]string(nil), windowsPayloadFileNames[:]...)
}

// ValidWindowsPayloadTreeName reports whether name is a shell tree entry.
func ValidWindowsPayloadTreeName(name string) bool {
	return strings.HasPrefix(name, WindowsPayloadTreePrefix) && installlayout.ValidateMemberName(name) == nil
}

// WindowsPayloadVersionMembers lists the manifest entries published under
// versions/<version>/, sorted: desktop, CLI, update helper and the app/ tree.
func WindowsPayloadVersionMembers(hashes map[string]string) []string {
	members := make([]string, 0, len(hashes))
	for name := range hashes {
		if slices.Contains(windowsPayloadVersionFileNames[:], name) || ValidWindowsPayloadTreeName(name) {
			members = append(members, name)
		}
	}
	slices.Sort(members)
	return members
}

func EncodeWindowsPayloadManifest(version string, hashes map[string]string) ([]byte, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, fmt.Errorf("Windows payload manifest version is empty")
	}
	manifest := WindowsPayloadManifest{
		SchemaVersion: WindowsPayloadManifestSchemaVersion,
		Version:       version,
		Files:         make([]WindowsPayloadManifestFile, 0, len(hashes)),
	}
	for _, name := range slices.Sorted(maps.Keys(hashes)) {
		if !windowsPayloadMemberAllowed(name, WindowsPayloadManifestSchemaVersion) {
			return nil, fmt.Errorf("Windows payload manifest contains unexpected file %q", name)
		}
		hash := strings.ToLower(strings.TrimSpace(hashes[name]))
		if !validWindowsPayloadSHA256(hash) {
			return nil, fmt.Errorf("Windows payload manifest hash for %s is invalid", name)
		}
		manifest.Files = append(manifest.Files, WindowsPayloadManifestFile{
			Name:   name,
			SHA256: hash,
		})
	}
	if err := requireWindowsPayloadFlatMembers(hashes); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func DecodeWindowsPayloadManifest(data []byte, expectedVersion string) (map[string]string, error) {
	var manifest WindowsPayloadManifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode Windows payload manifest: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode Windows payload manifest: trailing JSON value")
		}
		return nil, fmt.Errorf("decode Windows payload manifest: %w", err)
	}
	expectedVersion = strings.TrimSpace(expectedVersion)
	if !windowsPayloadSchemaSupported(manifest.SchemaVersion) ||
		expectedVersion == "" ||
		manifest.Version != expectedVersion {
		return nil, fmt.Errorf("Windows payload manifest identity does not match the pending update")
	}
	hashes := make(map[string]string, len(manifest.Files))
	for _, file := range manifest.Files {
		name := file.Name
		hash := file.SHA256
		if !windowsPayloadMemberAllowed(name, manifest.SchemaVersion) || !validWindowsPayloadSHA256(hash) {
			return nil, fmt.Errorf("Windows payload manifest member is invalid")
		}
		if _, duplicate := hashes[name]; duplicate {
			return nil, fmt.Errorf("Windows payload manifest contains duplicate members")
		}
		hashes[name] = hash
	}
	if err := requireWindowsPayloadFlatMembers(hashes); err != nil {
		return nil, err
	}
	return hashes, nil
}

func windowsPayloadSchemaSupported(schemaVersion int) bool {
	return schemaVersion == windowsPayloadFlatSchemaVersion || schemaVersion == WindowsPayloadManifestSchemaVersion
}

func windowsPayloadMemberAllowed(name string, schemaVersion int) bool {
	if slices.Contains(windowsPayloadFileNames[:], name) {
		return true
	}
	return schemaVersion >= WindowsPayloadManifestSchemaVersion && ValidWindowsPayloadTreeName(name)
}

func requireWindowsPayloadFlatMembers(hashes map[string]string) error {
	for _, name := range windowsPayloadFileNames {
		if _, ok := hashes[name]; !ok {
			return fmt.Errorf("Windows payload manifest is incomplete")
		}
	}
	return nil
}

func WindowsPayloadSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validWindowsPayloadSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	if value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
