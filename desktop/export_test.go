package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestSaveExportFileWritesTextAndBinaryPayloads(t *testing.T) {
	t.Parallel()
	app := &App{}
	dir := t.TempDir()

	textPath := filepath.Join(dir, "session.md")
	if err := app.SaveExportFile(textPath, "# 会话\n", false); err != nil {
		t.Fatalf("save text export: %v", err)
	}
	text, err := os.ReadFile(textPath)
	if err != nil {
		t.Fatalf("read text export: %v", err)
	}
	if got, want := string(text), "# 会话\n"; got != want {
		t.Fatalf("text export = %q, want %q", got, want)
	}

	binaryPath := filepath.Join(dir, "session.png")
	binary := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0xff}
	if err := app.SaveExportFile(binaryPath, base64.StdEncoding.EncodeToString(binary), true); err != nil {
		t.Fatalf("save binary export: %v", err)
	}
	written, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read binary export: %v", err)
	}
	if string(written) != string(binary) {
		t.Fatalf("binary export = %v, want %v", written, binary)
	}
}

func TestSaveExportFileRejectsInvalidBase64(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "broken.pdf")
	err := (&App{}).SaveExportFile(path, "not base64!", true)
	if err == nil {
		t.Fatal("expected invalid base64 error")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("invalid payload should not create a file, stat error = %v", statErr)
	}
}

func TestExportErrorsDoNotExposeSelectedDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missingDir := filepath.Join(dir, "private-export-directory")
	payload := base64.StdEncoding.EncodeToString([]byte("image"))
	tests := []struct {
		name string
		path string
		run  func(string) error
	}{
		{
			name: "single file",
			path: filepath.Join(missingDir, "session.pdf"),
			run: func(path string) error {
				return (&App{}).SaveExportFile(path, payload, true)
			},
		},
		{
			name: "multipart image",
			path: filepath.Join(missingDir, "session.png"),
			run: func(path string) error {
				return (&App{}).SaveExportImageFiles(path, []string{payload, payload})
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := test.run(test.path)
			if err == nil {
				t.Fatal("expected missing export directory to fail")
			}
			if strings.Contains(err.Error(), dir) {
				t.Fatalf("export error exposed selected directory: %q", err)
			}
			if !strings.Contains(err.Error(), "session") {
				t.Fatalf("export error should retain a safe file name: %q", err)
			}
		})
	}
}

func TestSaveExportImageFilesWritesNumberedParts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.archive.png")
	payloads := [][]byte{{0x01, 0x02}, {0x03, 0x04}, {0x05, 0x06}}
	encoded := make([]string, len(payloads))
	for i, payload := range payloads {
		encoded[i] = base64.StdEncoding.EncodeToString(payload)
	}

	if err := (&App{}).SaveExportImageFiles(path, encoded); err != nil {
		t.Fatalf("save image parts: %v", err)
	}
	for i, want := range payloads {
		partPath := filepath.Join(dir, fmt.Sprintf("session.archive-%d-of-3.png", i+1))
		got, err := os.ReadFile(partPath)
		if err != nil {
			t.Fatalf("read image part %d: %v", i+1, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("image part %d = %v, want %v", i+1, got, want)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("multi-part export should not write the selected base path, stat error = %v", err)
	}
}

func TestSaveExportImageFilesPreservesSelectedPath(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows normalizes trailing spaces in file names")
	}
	dir := t.TempDir()
	selectedPath := filepath.Join(dir, "session.png ")
	neighborPath := filepath.Join(dir, "session.png")
	if err := os.WriteFile(neighborPath, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("seed neighboring file: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString([]byte("new image"))

	if err := (&App{}).SaveExportImageFiles(selectedPath, []string{payload}); err != nil {
		t.Fatalf("save exact selected path: %v", err)
	}
	if got, err := os.ReadFile(selectedPath); err != nil || string(got) != "new image" {
		t.Fatalf("selected path data = %q, err = %v", got, err)
	}
	if got, err := os.ReadFile(neighborPath); err != nil || string(got) != "keep me" {
		t.Fatalf("neighboring file changed: data=%q err=%v", got, err)
	}
}

func TestSaveExportImageFilesMatchesNormalExportPermissions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	referencePath := filepath.Join(dir, "reference.png")
	if err := (&App{}).SaveExportFile(referencePath, base64.StdEncoding.EncodeToString([]byte("reference")), true); err != nil {
		t.Fatalf("save reference export: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString([]byte("image"))
	if err := (&App{}).SaveExportImageFiles(filepath.Join(dir, "session.png"), []string{payload, payload}); err != nil {
		t.Fatalf("save multipart export: %v", err)
	}

	referenceInfo, err := os.Stat(referencePath)
	if err != nil {
		t.Fatalf("stat reference export: %v", err)
	}
	partInfo, err := os.Stat(filepath.Join(dir, "session-1-of-2.png"))
	if err != nil {
		t.Fatalf("stat multipart export: %v", err)
	}
	if got, want := partInfo.Mode().Perm(), referenceInfo.Mode().Perm(); got != want {
		t.Fatalf("multipart permissions = %v, want normal export permissions %v", got, want)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, ".reasonix-export-*")); err != nil || len(matches) != 0 {
		t.Fatalf("staged files remain after successful export: matches=%v err=%v", matches, err)
	}
}

func TestSaveExportImageFilesRejectsCollisionWithoutPartialOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.png")
	collisionPath := filepath.Join(dir, "session-2-of-3.png")
	if err := os.WriteFile(collisionPath, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("seed collision: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString([]byte("new image"))

	err := (&App{}).SaveExportImageFiles(path, []string{payload, payload, payload})
	if err == nil {
		t.Fatal("expected existing numbered export to reject the batch")
	}
	if got, readErr := os.ReadFile(collisionPath); readErr != nil || string(got) != "keep me" {
		t.Fatalf("existing image part changed: data=%q err=%v", got, readErr)
	}
	for _, name := range []string{"session-1-of-3.png", "session-3-of-3.png"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("collision should leave no partial output %s, stat error = %v", name, statErr)
		}
	}
}

func TestSaveExportImageFilesDecodesAllPartsBeforeWriting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.png")
	valid := base64.StdEncoding.EncodeToString([]byte("image"))

	err := (&App{}).SaveExportImageFiles(path, []string{valid, "not base64!", valid})
	if err == nil {
		t.Fatal("expected invalid image payload to reject the batch")
	}
	for i := 1; i <= 3; i++ {
		partPath := filepath.Join(dir, fmt.Sprintf("session-%d-of-3.png", i))
		if _, statErr := os.Stat(partPath); !os.IsNotExist(statErr) {
			t.Fatalf("invalid payload should leave no image part %d, stat error = %v", i, statErr)
		}
	}
	if matches, globErr := filepath.Glob(filepath.Join(dir, ".reasonix-export-*")); globErr != nil || len(matches) != 0 {
		t.Fatalf("invalid payload left staged files: matches=%v err=%v", matches, globErr)
	}
}

func TestSaveExclusiveExportFilesRollsBackCommittedTargets(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "duplicate.png")

	err := saveExclusiveExportFiles(
		[]string{target, target},
		[][]byte{[]byte("first"), []byte("second")},
	)
	if err == nil {
		t.Fatal("expected duplicate exclusive target to fail")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("failed batch should roll back its committed target, stat error = %v", statErr)
	}
}

func TestRollbackDoesNotRemoveReplacedExportTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tempPath := filepath.Join(dir, ".staged.png")
	targetPath := filepath.Join(dir, "session.png")
	if err := os.WriteFile(tempPath, []byte("staged"), 0o644); err != nil {
		t.Fatalf("write staged file: %v", err)
	}
	created, err := commitStagedExportFile(tempPath, targetPath)
	if err != nil {
		t.Fatalf("commit staged file: %v", err)
	}
	tempInfo, err := os.Lstat(tempPath)
	if err != nil {
		t.Fatalf("stat staged file: %v", err)
	}
	if !os.SameFile(created, tempInfo) {
		t.Fatal("commit must return the staged inode identity")
	}
	if err := os.Remove(targetPath); err != nil {
		t.Fatalf("replace committed target: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte("replacement"), 0o644); err != nil {
		t.Fatalf("write replacement target: %v", err)
	}

	rollbackCommittedExportFiles([]committedExportFile{{path: targetPath, info: created}})
	if got, err := os.ReadFile(targetPath); err != nil || string(got) != "replacement" {
		t.Fatalf("rollback removed replacement: data=%q err=%v", got, err)
	}
}

func TestConcurrentMultipartExportsHaveSingleCompleteWinner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.png")
	encode := func(values ...string) []string {
		encoded := make([]string, len(values))
		for i, value := range values {
			encoded[i] = base64.StdEncoding.EncodeToString([]byte(value))
		}
		return encoded
	}
	batches := [][]string{
		encode("a-1", "a-2", "a-3"),
		encode("b-1", "b-2", "b-3"),
	}
	start := make(chan struct{})
	errs := make(chan error, len(batches))
	var ready sync.WaitGroup
	ready.Add(len(batches))
	for _, batch := range batches {
		batch := batch
		go func() {
			ready.Done()
			<-start
			errs <- (&App{}).SaveExportImageFiles(path, batch)
		}()
	}
	ready.Wait()
	close(start)

	successes := 0
	for range batches {
		if err := <-errs; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent exports = %d, want exactly one", successes)
	}
	first, err := os.ReadFile(filepath.Join(dir, "session-1-of-3.png"))
	if err != nil {
		t.Fatalf("read winning first part: %v", err)
	}
	winner := string(first[:1])
	for i := 1; i <= 3; i++ {
		got, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("session-%d-of-3.png", i)))
		if err != nil {
			t.Fatalf("read winning part %d: %v", i, err)
		}
		if want := fmt.Sprintf("%s-%d", winner, i); string(got) != want {
			t.Fatalf("winning part %d = %q, want %q from one batch", i, got, want)
		}
	}
	if matches, globErr := filepath.Glob(filepath.Join(dir, ".reasonix-export-*")); globErr != nil || len(matches) != 0 {
		t.Fatalf("concurrent export left staged files: matches=%v err=%v", matches, globErr)
	}
}

func TestExportFileFiltersSelectExpectedNativePattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mime string
		ext  string
		want string
	}{
		{mime: "application/pdf", ext: ".pdf", want: "*.pdf"},
		{mime: "image/png", ext: ".png", want: "*.png"},
		{mime: "application/octet-stream", ext: ".bin", want: "*.bin"},
	}
	for _, test := range tests {
		filters := exportFileFilters(test.mime, test.ext)
		if len(filters) != 1 || filters[0].Pattern != test.want {
			t.Fatalf("filters for %s = %#v, want pattern %q", test.mime, filters, test.want)
		}
	}
}
