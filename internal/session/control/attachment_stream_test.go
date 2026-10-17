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

func TestAttachmentStreamCleansIncompleteFiles(t *testing.T) {
	for _, kind := range []string{"empty", "read failure", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var src io.Reader = strings.NewReader("")
			if kind != "empty" {
				src = io.MultiReader(strings.NewReader("part of apk"), failedAttachmentReader{cancel: cancel, canceled: kind == "cancel"})
			}
			if _, err := SaveAttachmentStreamInRoot(ctx, root, "app.apk", src); err == nil {
				t.Fatal("accepted an incomplete upload")
			}
			entries, err := os.ReadDir(filepath.Join(root, ".reasonix", "attachments"))
			if err != nil || len(entries) != 1 || entries[0].Name() != ".gitignore" {
				t.Fatalf("partial uploads: %v, %v", entries, err)
			}
		})
	}
}

type failedAttachmentReader struct {
	cancel   context.CancelFunc
	canceled bool
}

func TestAttachmentStreamReadsLargeFilesInBoundedChunks(t *testing.T) {
	root := t.TempDir()
	const size = 32 << 20
	reader := &boundedAttachmentReader{left: size}
	ref, err := SaveAttachmentStreamInRoot(t.Context(), root, "large.apk", reader)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, ref))
	if err != nil || info.Size() != size {
		t.Fatalf("saved file %v, %v", info, err)
	}
}

type boundedAttachmentReader struct{ left int }

func (r *boundedAttachmentReader) Read(p []byte) (int, error) {
	if len(p) > 64<<10 {
		return 0, errors.New("attachment reader allocated an unbounded chunk")
	}
	if r.left == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.left)
	clear(p[:n])
	r.left -= n
	return n, nil
}

func (r failedAttachmentReader) Read([]byte) (int, error) {
	if r.canceled {
		r.cancel()
		return 0, context.Canceled
	}
	return 0, errors.New("fixture read failure")
}
