//go:build windows

package winsandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const capabilityRecordVersion = 1

type capabilityRecordStore struct {
	dir string
}

type capabilityRecord struct {
	Version       int               `json:"version"`
	Status        string            `json:"status"`
	Purpose       capabilityPurpose `json:"purpose"`
	CanonicalPath string            `json:"canonicalPath"`
	SID           string            `json:"sid"`
	AccessMask    uint32            `json:"accessMask"`
	Inheritance   uint32            `json:"inheritance"`
	VolumeSerial  uint32            `json:"volumeSerial"`
	FileIndexHigh uint32            `json:"fileIndexHigh"`
	FileIndexLow  uint32            `json:"fileIndexLow"`
	OwnerPID      int               `json:"ownerPid"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

func newCapabilityRecordStore(protectedRoots, writableRoots []string) (*capabilityRecordStore, error) {
	for _, root := range protectedRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve capability record root %q: %w", root, err)
		}
		if err := os.MkdirAll(abs, 0o700); err != nil {
			return nil, fmt.Errorf("create capability record root %q: %w", abs, err)
		}
		canonical, err := canonicalWindowsDirectory(abs)
		if err != nil {
			return nil, fmt.Errorf("canonicalize capability record root %q: %w", abs, err)
		}
		dir := filepath.Join(canonical, "windows-sandbox-capabilities-v1")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create capability record directory %q: %w", dir, err)
		}
		canonicalDir, err := canonicalWindowsDirectory(dir)
		if err != nil {
			return nil, fmt.Errorf("validate capability record directory %q: %w", dir, err)
		}
		if !windowsPathWithin(canonical, canonicalDir) {
			return nil, fmt.Errorf("capability record directory %q escapes protected state root %q", canonicalDir, canonical)
		}
		for _, writable := range writableRoots {
			if windowsPathsOverlap(canonicalDir, writable) {
				return nil, fmt.Errorf("capability record directory %q overlaps writable root %q", canonicalDir, writable)
			}
		}
		return &capabilityRecordStore{dir: canonicalDir}, nil
	}
	return nil, nil
}

func (s *capabilityRecordStore) write(capability restrictedCapability, status string) error {
	if s == nil || capability.purpose != capabilityWorkspace {
		return nil
	}
	record := capabilityRecord{
		Version:       capabilityRecordVersion,
		Status:        status,
		Purpose:       capability.purpose,
		CanonicalPath: capability.root,
		SID:           capability.sidText,
		AccessMask:    uint32(capabilityWriteGrantMask),
		Inheritance:   uint32(windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT),
		VolumeSerial:  capability.object.volumeSerial,
		FileIndexHigh: capability.object.indexHigh,
		FileIndexLow:  capability.object.indexLow,
		OwnerPID:      os.Getpid(),
		UpdatedAt:     time.Now().UTC(),
	}
	nameHash := sha256.Sum256([]byte(capability.sidText))
	path := filepath.Join(s.dir, hex.EncodeToString(nameHash[:16])+".json")
	tmp, err := os.CreateTemp(s.dir, ".capability-*.tmp")
	if err != nil {
		return fmt.Errorf("create capability record: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("protect capability record: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		return fmt.Errorf("encode capability record: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("flush capability record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close capability record: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish capability record: %w", err)
	}
	cleanup = false
	return nil
}
