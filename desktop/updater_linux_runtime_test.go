//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyLinuxArchiveReplacesBinaryAndRuntime(t *testing.T) {
	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "chromium")
	writeLinuxRuntimeMarker(t, runtimeRoot, "old")
	archive := linuxUpdateArchive(t, []byte("new-binary"), "new")

	var applied []byte
	err := applyLinuxArchive(archive, runtimeRoot, func(binary []byte) error {
		applied = append([]byte(nil), binary...)
		return nil
	}, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("applyLinuxArchive: %v", err)
	}
	if string(applied) != "new-binary" {
		t.Fatalf("applied binary = %q", applied)
	}
	marker, err := os.ReadFile(filepath.Join(runtimeRoot, "runtime-marker"))
	if err != nil || string(marker) != "new" {
		t.Fatalf("installed runtime marker = %q, %v", marker, err)
	}
}

func TestApplyLinuxArchiveRollsBackRuntimeWhenBinaryFails(t *testing.T) {
	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "chromium")
	writeLinuxRuntimeMarker(t, runtimeRoot, "old")
	archive := linuxUpdateArchive(t, []byte("new-binary"), "new")

	err := applyLinuxArchive(archive, runtimeRoot, func([]byte) error {
		return nil
	}, func() error {
		return errors.New("simulated binary replacement failure")
	})
	if err == nil {
		t.Fatal("applyLinuxArchive succeeded after binary replacement failed")
	}
	marker, readErr := os.ReadFile(filepath.Join(runtimeRoot, "runtime-marker"))
	if readErr != nil || string(marker) != "old" {
		t.Fatalf("runtime rollback marker = %q, %v", marker, readErr)
	}
	if _, statErr := os.Stat(runtimeRoot + ".reasonix-update-backup"); !os.IsNotExist(statErr) {
		t.Fatalf("runtime backup remained after rollback: %v", statErr)
	}
}

func writeLinuxRuntimeMarker(t *testing.T, root, value string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runtime-marker"), []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func linuxUpdateArchive(t *testing.T, binary []byte, marker string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	entries := []struct {
		name string
		mode int64
		body []byte
		dir  bool
	}{
		{name: "reasonix-desktop", mode: 0o755, body: binary},
		{name: "chromium", mode: 0o755, dir: true},
		{name: "chromium/chrome", mode: 0o755, body: []byte("chrome")},
		{name: "chromium/resources.pak", mode: 0o644, body: []byte("resources")},
		{name: "chromium/icudtl.dat", mode: 0o644, body: []byte("icu")},
		{name: "chromium/locales", mode: 0o755, dir: true},
		{name: "chromium/locales/en-US.pak", mode: 0o644, body: []byte("locale")},
		{name: "chromium/runtime-marker", mode: 0o644, body: []byte(marker)},
	}
	for _, entry := range entries {
		typeflag := byte(tar.TypeReg)
		if entry.dir {
			typeflag = tar.TypeDir
		}
		header := &tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.body)), Typeflag: typeflag}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if len(entry.body) > 0 {
			if _, err := tarWriter.Write(entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}
