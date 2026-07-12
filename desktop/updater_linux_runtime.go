package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/minio/selfupdate"
)

type linuxUpdatePayload struct {
	binary     []byte
	runtimeDir string
	stagingDir string
}

func applyLinux(targz []byte) error {
	executable := currentExecutablePath()
	if executable == "" {
		return errors.New("update: current Linux executable is unavailable")
	}
	runtimeRoot := linuxRuntimePathForExecutable(executable)
	backup := executable + ".reasonix-update-backup"
	newBinary := filepath.Join(filepath.Dir(executable), "."+filepath.Base(executable)+".new")
	_ = os.Remove(newBinary)
	_ = os.Remove(backup)
	options := selfupdate.Options{TargetPath: executable, TargetMode: 0o755, OldSavePath: backup}
	var commitErr error
	err := applyLinuxArchive(targz, runtimeRoot, func(binary []byte) error {
		return selfupdate.PrepareAndCheckBinary(bytes.NewReader(binary), options)
	}, func() error {
		commitErr = selfupdate.CommitBinary(options)
		return commitErr
	})
	if err != nil {
		_ = os.Remove(newBinary)
		if commitErr == nil || selfupdate.RollbackError(commitErr) == nil {
			_ = os.Remove(backup)
		}
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func linuxRuntimePathForExecutable(executable string) string {
	executable = filepath.Clean(executable)
	if filepath.ToSlash(filepath.Dir(executable)) == "/usr/bin" {
		return "/usr/lib/reasonix/chromium"
	}
	return filepath.Join(filepath.Dir(executable), "chromium")
}

func applyLinuxArchive(targz []byte, runtimeRoot string, prepareBinary func([]byte) error, commitBinary func() error) error {
	if runtimeRoot == "" || prepareBinary == nil || commitBinary == nil {
		return errors.New("update: invalid Linux update target")
	}
	payload, err := extractLinuxUpdate(targz, filepath.Dir(runtimeRoot))
	if err != nil {
		return err
	}
	defer os.RemoveAll(payload.stagingDir)
	if err := prepareBinary(payload.binary); err != nil {
		return fmt.Errorf("update: prepare Reasonix binary: %w", err)
	}

	backup := runtimeRoot + ".reasonix-update-backup"
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("update: remove stale Chromium backup: %w", err)
	}
	hadRuntime := false
	if _, err := os.Stat(runtimeRoot); err == nil {
		hadRuntime = true
		if err := os.Rename(runtimeRoot, backup); err != nil {
			return fmt.Errorf("update: back up bundled Chromium: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	restore := func() {
		_ = os.RemoveAll(runtimeRoot)
		if hadRuntime {
			_ = os.Rename(backup, runtimeRoot)
		}
	}
	if err := os.Rename(payload.runtimeDir, runtimeRoot); err != nil {
		restore()
		return fmt.Errorf("update: install bundled Chromium: %w", err)
	}
	if err := commitBinary(); err != nil {
		restore()
		return fmt.Errorf("update: replace Reasonix binary: %w", err)
	}
	_ = os.RemoveAll(backup)
	return nil
}

func extractLinuxUpdate(targz []byte, stagingParent string) (linuxUpdatePayload, error) {
	if err := os.MkdirAll(stagingParent, 0o755); err != nil {
		return linuxUpdatePayload{}, fmt.Errorf("update: create Chromium parent: %w", err)
	}
	staging, err := os.MkdirTemp(stagingParent, ".reasonix-update-*")
	if err != nil {
		return linuxUpdatePayload{}, fmt.Errorf("update: create Linux update staging: %w", err)
	}
	payload := linuxUpdatePayload{runtimeDir: filepath.Join(staging, "chromium"), stagingDir: staging}
	fail := func(err error) (linuxUpdatePayload, error) {
		_ = os.RemoveAll(staging)
		return linuxUpdatePayload{}, err
	}

	gz, err := gzip.NewReader(bytes.NewReader(targz))
	if err != nil {
		return fail(fmt.Errorf("update: open Linux archive: %w", err))
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	foundRuntime := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(fmt.Errorf("update: read Linux archive: %w", err))
		}
		name := strings.TrimPrefix(strings.ReplaceAll(header.Name, "\\", "/"), "./")
		clean := path.Clean(name)
		if clean == "reasonix-desktop" {
			if header.Typeflag != tar.TypeReg {
				return fail(errors.New("update: reasonix-desktop is not a regular file"))
			}
			payload.binary, err = io.ReadAll(reader)
			if err != nil {
				return fail(err)
			}
			continue
		}
		if clean == "chromium" {
			foundRuntime = true
			if err := os.MkdirAll(payload.runtimeDir, 0o755); err != nil {
				return fail(err)
			}
			continue
		}
		if !strings.HasPrefix(clean, "chromium/") || clean == "chromium/.." || strings.Contains(clean, "/../") {
			return fail(fmt.Errorf("update: unexpected archive entry %q", header.Name))
		}
		foundRuntime = true
		relative := strings.TrimPrefix(clean, "chromium/")
		destination := filepath.Join(payload.runtimeDir, filepath.FromSlash(relative))
		if !pathWithin(payload.runtimeDir, destination) {
			return fail(fmt.Errorf("update: unsafe Chromium archive entry %q", header.Name))
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destination, os.FileMode(header.Mode).Perm()); err != nil {
				return fail(err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return fail(err)
			}
			file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode).Perm())
			if err != nil {
				return fail(err)
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return fail(copyErr)
			}
			if closeErr != nil {
				return fail(closeErr)
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(header.Linkname) {
				return fail(fmt.Errorf("update: unsafe Chromium symlink %q", header.Name))
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(destination), filepath.FromSlash(header.Linkname)))
			if !pathWithin(payload.runtimeDir, resolved) {
				return fail(fmt.Errorf("update: Chromium symlink escapes runtime %q", header.Name))
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return fail(err)
			}
			if err := os.Symlink(header.Linkname, destination); err != nil {
				return fail(err)
			}
		default:
			return fail(fmt.Errorf("update: unsupported Chromium archive entry %q", header.Name))
		}
	}
	if len(payload.binary) == 0 {
		return fail(errors.New("update: reasonix-desktop is missing from archive"))
	}
	if !foundRuntime {
		return fail(errors.New("update: bundled Chromium is missing from archive"))
	}
	for _, required := range []string{"chrome", "resources.pak", "icudtl.dat", "locales"} {
		if _, err := os.Stat(filepath.Join(payload.runtimeDir, required)); err != nil {
			return fail(fmt.Errorf("update: bundled Chromium resource %q is missing", required))
		}
	}
	if info, err := os.Stat(filepath.Join(payload.runtimeDir, "chrome")); err != nil || info.Mode().Perm()&0o111 == 0 {
		return fail(errors.New("update: bundled Chromium executable is not executable"))
	}
	return payload, nil
}

func pathWithin(root, name string) bool {
	relative, err := filepath.Rel(root, name)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
