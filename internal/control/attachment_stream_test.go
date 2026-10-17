package control

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type attachmentPatternReader struct {
	remaining int64
	maxRead   int
}

func (r *attachmentPatternReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), r.remaining))
	for i := range p[:n] {
		p[i] = byte(i % 251)
	}
	r.remaining -= int64(n)
	return n, nil
}

func TestSaveAttachmentStreamLargeFileUsesBoundedReads(t *testing.T) {
	root := t.TempDir()
	const size = 80 << 20
	reader := &attachmentPatternReader{remaining: size}
	path, err := SaveAttachmentStreamInRoot(context.Background(), root, "base.apk", reader)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil || info.Size() != size || !strings.HasSuffix(path, ".apk") {
		t.Fatalf("file=%v err=%v path=%s", info, err, path)
	}
	if reader.maxRead > 64<<10 {
		t.Fatalf("unbounded read: %d", reader.maxRead)
	}
}

type failingAttachmentReader struct{ cancel context.CancelFunc }

func (r *failingAttachmentReader) Read(p []byte) (int, error) {
	if r.cancel != nil {
		r.cancel()
		return copy(p, "partial"), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func TestSaveAttachmentStreamCleansInterruptedWrites(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "read failure", true: "cancelled"}[cancelled], func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := &failingAttachmentReader{}
			want := io.ErrUnexpectedEOF
			if cancelled {
				reader.cancel = cancel
				want = context.Canceled
			}
			if _, err := SaveAttachmentStreamInRoot(ctx, root, "bad.apk", reader); !errors.Is(err, want) {
				t.Fatalf("err=%v", err)
			}
			entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("partial files=%v err=%v", entries, err)
			}
		})
	}
}

func TestSaveAttachmentStreamRejectsEmptyFile(t *testing.T) {
	root := t.TempDir()
	if _, err := SaveAttachmentStreamInRoot(context.Background(), root, "empty.apk", strings.NewReader("")); err == nil {
		t.Fatal("accepted empty file")
	}
	entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
}

func TestExternalFileReferenceDoesNotReadOrAuthorizeParent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "large app.apk")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(120 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	c := newOwnedTestController(t, Options{WorkspaceRoot: root})
	token, display, err := c.RegisterExternalFileRef(path)
	if err != nil {
		t.Fatal(err)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if display != filepath.ToSlash(realPath) {
		t.Fatalf("display=%q", display)
	}
	block, errs := c.ResolveRefs(context.Background(), "inspect @"+EscapeRefPath(token))
	if len(errs) != 0 || !strings.Contains(block, "binary file") || len(block) > 4096 {
		t.Fatalf("block=%q errs=%v", block, errs)
	}
	for _, sub := range []string{"/../secret", "/other.apk", "/.."} {
		if _, ok := c.externalFolderRef(token + sub); ok {
			t.Fatalf("file reference authorized subpath: %s", sub)
		}
	}
}
