package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	WindowsPayloadManifestSchemaVersion = 1
	WindowsPayloadManifestName          = "reasonix-payload.json"
	WindowsPayloadSignatureName         = WindowsPayloadManifestName + ".minisig"
)

var windowsPayloadFileNames = [...]string{
	"reasonix-desktop.exe",
	"reasonix-guard.exe",
	"reasonix-launcher.exe",
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

func EncodeWindowsPayloadManifest(version string, hashes map[string]string) ([]byte, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, fmt.Errorf("Windows payload manifest version is empty")
	}
	manifest := WindowsPayloadManifest{
		SchemaVersion: WindowsPayloadManifestSchemaVersion,
		Version:       version,
		Files:         make([]WindowsPayloadManifestFile, 0, len(windowsPayloadFileNames)),
	}
	for _, name := range windowsPayloadFileNames {
		hash := strings.ToLower(strings.TrimSpace(hashes[name]))
		if !validWindowsPayloadSHA256(hash) {
			return nil, fmt.Errorf("Windows payload manifest hash for %s is invalid", name)
		}
		manifest.Files = append(manifest.Files, WindowsPayloadManifestFile{
			Name:   name,
			SHA256: hash,
		})
	}
	if len(hashes) != len(manifest.Files) {
		return nil, fmt.Errorf("Windows payload manifest contains unexpected files")
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
	if manifest.SchemaVersion != WindowsPayloadManifestSchemaVersion ||
		expectedVersion == "" ||
		manifest.Version != expectedVersion {
		return nil, fmt.Errorf("Windows payload manifest identity does not match the pending update")
	}
	expected := make(map[string]struct{}, len(windowsPayloadFileNames))
	for _, name := range windowsPayloadFileNames {
		expected[name] = struct{}{}
	}
	hashes := make(map[string]string, len(manifest.Files))
	for _, file := range manifest.Files {
		name := file.Name
		hash := file.SHA256
		if _, ok := expected[name]; !ok || !validWindowsPayloadSHA256(hash) {
			return nil, fmt.Errorf("Windows payload manifest member is invalid")
		}
		if _, duplicate := hashes[name]; duplicate {
			return nil, fmt.Errorf("Windows payload manifest contains duplicate members")
		}
		hashes[name] = hash
	}
	if len(hashes) != len(expected) {
		return nil, fmt.Errorf("Windows payload manifest is incomplete")
	}
	return hashes, nil
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
