package runtimeservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/runtimeapi"
)

var testSession = runtimeapi.SessionRef{WorkspaceID: "ws_test", SessionID: "session_test"}

func newTestService(t *testing.T, root string, checkpoints CheckpointChangeProvider, gitBinary string) *FileGitService {
	t.Helper()
	service, err := NewFileGitService(Options{Root: root, Checkpoints: checkpoints, GitBinary: gitBinary})
	if err != nil {
		t.Fatalf("NewFileGitService: %v", err)
	}
	return service
}

func writeTestFile(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", rel, err)
	}
	if err := os.WriteFile(name, body, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", rel, err)
	}
}

func TestListFilesSafePaginationNoiseAndCursorRevision(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, root, "a.txt", []byte("a"))
	writeTestFile(t, root, "b.txt", []byte("b"))
	writeTestFile(t, root, "src/main.go", []byte("package main"))
	writeTestFile(t, root, "node_modules/pkg/noise.js", []byte("noise"))
	writeTestFile(t, root, ".git/HEAD", []byte("secret"))
	writeTestFile(t, outside, "outside.txt", []byte("outside"))
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "safe-link")); err != nil {
		t.Fatalf("safe symlink: %v", err)
	}
	service := newTestService(t, root, nil, "")

	first, err := service.ListFiles(context.Background(), runtimeapi.FileListInput{Session: testSession, Limit: 2})
	if err != nil {
		t.Fatalf("ListFiles first: %v", err)
	}
	if !first.HasMore || first.Next == "" || len(first.Entries) != 2 {
		t.Fatalf("first page = %+v", first)
	}
	if !first.Entries[0].IsDir || !first.Entries[1].IsDir {
		t.Fatalf("directories must sort first: %+v", first.Entries)
	}
	for _, entry := range first.Entries {
		if entry.Name == "escape" || entry.Name == "node_modules" || entry.Name == ".git" {
			t.Fatalf("unsafe/noise entry leaked: %+v", entry)
		}
	}
	second, err := service.ListFiles(context.Background(), runtimeapi.FileListInput{Session: testSession, Cursor: first.Next, Limit: 100})
	if err != nil {
		t.Fatalf("ListFiles second: %v", err)
	}
	if second.HasMore || second.Next != "" || len(second.Entries) != 2 {
		t.Fatalf("second page = %+v", second)
	}

	tampered := string(first.Next)
	last := tampered[len(tampered)-1]
	if last == 'A' {
		last = 'B'
	} else {
		last = 'A'
	}
	tampered = tampered[:len(tampered)-1] + string(last)
	_, err = service.ListFiles(context.Background(), runtimeapi.FileListInput{Session: testSession, Cursor: runtimeapi.Cursor(tampered)})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("tampered cursor err = %v", err)
	}

	writeTestFile(t, root, "new.txt", []byte("new"))
	_, err = service.ListFiles(context.Background(), runtimeapi.FileListInput{Session: testSession, Cursor: first.Next})
	if !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("mutated snapshot cursor err = %v", err)
	}
}

func TestFileQueriesRejectTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, outside, "secret.txt", []byte("secret"))
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	service := newTestService(t, root, nil, "")

	for _, candidate := range []string{"../secret", "/etc/passwd", `C:/Windows`, `dir\\file`, "bad:name", "a/../../b"} {
		_, err := service.PreviewFile(context.Background(), runtimeapi.FilePreviewInput{Session: testSession, Path: candidate})
		if !errors.Is(err, ErrInvalidPath) && !errors.Is(err, ErrPathEscapesRoot) {
			t.Errorf("PreviewFile(%q) err = %v", candidate, err)
		}
	}
	_, err := service.PreviewFile(context.Background(), runtimeapi.FilePreviewInput{Session: testSession, Path: "secret-link"})
	if !errors.Is(err, ErrPathEscapesRoot) {
		t.Fatalf("escaped symlink err = %v", err)
	}
}

func TestSearchFilesNoiseResultLimitAndScanLimit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "src/needle-one.go", []byte(""))
	writeTestFile(t, root, "src/needle-two.go", []byte(""))
	writeTestFile(t, root, "needle-dir/inside.txt", []byte(""))
	writeTestFile(t, root, "node_modules/needle-noise.js", []byte(""))
	service := newTestService(t, root, nil, "")

	limited, err := service.SearchFiles(context.Background(), runtimeapi.FileSearchInput{Session: testSession, Query: "needle", Limit: 2})
	if err != nil {
		t.Fatalf("SearchFiles result limit: %v", err)
	}
	if !limited.Truncated || limited.TruncationReason != runtimeapi.SearchResultLimit || limited.ReturnedItems != 2 || limited.TotalItems == nil || *limited.TotalItems != 4 {
		t.Fatalf("limited search = %+v", limited)
	}
	for _, entry := range limited.Entries {
		if strings.Contains(entry.Path, "node_modules") {
			t.Fatalf("noise search entry leaked: %+v", entry)
		}
	}

	large := t.TempDir()
	for i := 0; i <= runtimeapi.SearchMaxVisitedItems; i++ {
		name := filepath.Join(large, fmt.Sprintf("file-%05d.txt", i))
		file, createErr := os.Create(name)
		if createErr != nil {
			t.Fatalf("create scan file %d: %v", i, createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			t.Fatalf("close scan file %d: %v", i, closeErr)
		}
	}
	largeService := newTestService(t, large, nil, "")
	scan, err := largeService.SearchFiles(context.Background(), runtimeapi.FileSearchInput{Session: testSession, Query: "not-present"})
	if err != nil {
		t.Fatalf("SearchFiles scan limit: %v", err)
	}
	if !scan.Truncated || scan.TruncationReason != runtimeapi.SearchScanLimit || scan.TotalItems != nil || scan.ReturnedItems != 0 {
		t.Fatalf("scan-limited search = %+v", scan)
	}
}

func TestPreviewFileBinaryMediaAndUTF8Boundary(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "binary.dat", []byte{'a', 0, 'b'})
	writeTestFile(t, root, "photo.png", []byte("not actually decoded"))
	writeTestFile(t, root, "empty.txt", nil)
	invalid := append(bytesRepeat('a', runtimeapi.PreviewBytes-1), 0xff, 'x')
	writeTestFile(t, root, "invalid.txt", invalid)
	prefix := bytesRepeat('a', runtimeapi.PreviewBytes-1)
	boundary := append(prefix, []byte("界")...)
	writeTestFile(t, root, "boundary.txt", boundary)
	service := newTestService(t, root, nil, "")

	binary, err := service.PreviewFile(context.Background(), runtimeapi.FilePreviewInput{Session: testSession, Path: "binary.dat"})
	if err != nil || binary.Kind != runtimeapi.FileBinary || !binary.Binary || binary.Body != nil || binary.Truncated {
		t.Fatalf("binary preview = %+v, err=%v", binary, err)
	}
	image, err := service.PreviewFile(context.Background(), runtimeapi.FilePreviewInput{Session: testSession, Path: "photo.png"})
	if err != nil || image.Kind != runtimeapi.FileImage || !image.Binary || image.ReturnedBytes != 0 || image.Body != nil {
		t.Fatalf("image preview = %+v, err=%v", image, err)
	}
	empty, err := service.PreviewFile(context.Background(), runtimeapi.FilePreviewInput{Session: testSession, Path: "empty.txt"})
	if err != nil || empty.Body == nil || *empty.Body != "" || empty.Truncated || empty.ReturnedBytes != 0 {
		t.Fatalf("empty preview = %+v, err=%v", empty, err)
	}
	invalidPreview, err := service.PreviewFile(context.Background(), runtimeapi.FilePreviewInput{Session: testSession, Path: "invalid.txt"})
	if err != nil || invalidPreview.Kind != runtimeapi.FileBinary || !invalidPreview.Binary {
		t.Fatalf("invalid UTF-8 preview = %+v, err=%v", invalidPreview, err)
	}
	preview, err := service.PreviewFile(context.Background(), runtimeapi.FilePreviewInput{Session: testSession, Path: "boundary.txt"})
	if err != nil {
		t.Fatalf("boundary preview: %v", err)
	}
	if preview.Body == nil || !utf8.ValidString(*preview.Body) || preview.ReturnedBytes != int64(runtimeapi.PreviewBytes-1) || preview.SizeBytes != int64(len(boundary)) || !preview.Truncated || preview.TruncationReason != runtimeapi.ByteLimit {
		t.Fatalf("boundary preview = %+v", preview)
	}
}

func bytesRepeat(value byte, count int) []byte {
	result := make([]byte, count)
	for i := range result {
		result[i] = value
	}
	return result
}

func TestFileGitServiceConcurrentQueries(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 32; i++ {
		writeTestFile(t, root, fmt.Sprintf("src/file-%02d.txt", i), []byte("text"))
	}
	service := newTestService(t, root, nil, "")
	t.Run("parallel", func(t *testing.T) {
		for i := 0; i < 16; i++ {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				if _, err := service.ListFiles(context.Background(), runtimeapi.FileListInput{Session: testSession, Path: "src"}); err != nil {
					t.Errorf("ListFiles: %v", err)
				}
				if _, err := service.SearchFiles(context.Background(), runtimeapi.FileSearchInput{Session: testSession, Query: "file"}); err != nil {
					t.Errorf("SearchFiles: %v", err)
				}
			})
		}
	})
}
