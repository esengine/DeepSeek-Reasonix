package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
)

const migrationBackupDir = ".migration-backup"

type MigrationBackupError struct {
	Path string
	Err  error
}

func (e *MigrationBackupError) Error() string {
	return fmt.Sprintf("memory.migration_backup: preserve %s: %v", e.Path, e.Err)
}

func (e *MigrationBackupError) Unwrap() error { return e.Err }

func backupLegacyMemory(dir, name string, raw []byte, version string) error {
	path := name
	if !validBackupName(name) {
		return &MigrationBackupError{Path: path, Err: os.ErrInvalid}
	}
	backup, err := openBackupRoot(dir)
	if err != nil {
		return &MigrationBackupError{Path: path, Err: err}
	}
	defer backup.Close()
	version = strings.TrimSpace(version)
	if version == "" {
		version = "dev"
	}
	manifest, err := json.MarshalIndent(struct {
		MigratedAt time.Time `json:"migrated_at"`
		AppVersion string    `json:"app_version"`
	}{time.Now().UTC(), version}, "", "  ")
	if err != nil {
		return &MigrationBackupError{Path: path, Err: err}
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{"manifest.json", append(manifest, '\n')},
		{name, raw},
	} {
		if err := createMigrationBackup(backup, file.name, file.data); err != nil {
			return &MigrationBackupError{Path: path, Err: err}
		}
	}
	return nil
}

func validBackupName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`) && !strings.ContainsRune(name, 0)
}

// openBackupRoot returns a handle confined to dir/.migration-backup; every
// backup file operation goes through it, so no name can resolve outside.
func openBackupRoot(dir string) (*os.Root, error) {
	parent, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err := parent.Mkdir(migrationBackupDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	info, err := parent.Lstat(migrationBackupDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, os.ErrInvalid
	}
	return parent.OpenRoot(migrationBackupDir)
}

func createMigrationBackup(root *os.Root, name string, data []byte) error {
	if !validBackupName(name) {
		return os.ErrInvalid
	}
	info, err := root.Lstat(name)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup is not a regular file: %w", os.ErrInvalid)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// Publishing only complete, fsynced files makes an interrupted migration
	// retryable without replacing the first copy of the original bytes.
	tmp := ".atomic-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(tmp) }()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := root.Link(tmp, name); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}
