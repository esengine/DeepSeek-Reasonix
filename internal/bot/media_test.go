package bot

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveInboundMediaStoresWorkspaceImageAttachment(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(raw)
	}))
	defer srv.Close()
	workspace := t.TempDir()

	ref, err := saveOneInboundMedia(context.Background(), workspace, srv.URL+"/shot.png")
	if err != nil {
		t.Fatalf("saveOneInboundMedia: %v", err)
	}
	if !strings.HasPrefix(ref, ".reasonix/attachments/") || !strings.HasSuffix(ref, ".png") {
		t.Fatalf("ref = %q, want png attachment ref", ref)
	}
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(ref))); err != nil {
		t.Fatalf("stored attachment missing: %v", err)
	}
}
