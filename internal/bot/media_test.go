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

func TestSaveInboundMediaItemsStoresBytesAndReportsErrors(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	workspace := t.TempDir()

	refs, fallbacks, errs := saveInboundMediaItems(context.Background(), workspace, []InboundMedia{
		{MIME: "image/png", Data: png},
		{Name: "notes.txt", MIME: "text/plain", Data: []byte("hello from feishu")},
		{Name: "empty.bin", FailureText: "[file unavailable]"}, // no data -> error
	})
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one for the empty item", errs)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %v, want image and text attachment", refs)
	}
	if len(fallbacks) != 1 || fallbacks[0] != "[file unavailable]" {
		t.Fatalf("fallbacks = %v, want the failed item's placeholder", fallbacks)
	}
	if !strings.HasSuffix(refs[0], ".png") {
		t.Fatalf("image ref = %q, want .png attachment", refs[0])
	}
	if !strings.HasSuffix(refs[1], ".txt") {
		t.Fatalf("text ref = %q, want .txt attachment", refs[1])
	}
	for _, ref := range refs {
		if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(ref))); err != nil {
			t.Fatalf("stored attachment missing: %v", err)
		}
	}
}

func TestSaveInboundMediaItemsLoadsDeferredBytesAfterAdmission(t *testing.T) {
	workspace := t.TempDir()
	called := false
	refs, fallbacks, errs := saveInboundMediaItems(context.Background(), workspace, []InboundMedia{{
		FailureText: "[download failed]",
		Load: func(context.Context) ([]byte, string, error) {
			called = true
			return []byte("deferred data"), "notes.txt", nil
		},
	}})
	if !called {
		t.Fatal("deferred loader was not called")
	}
	if len(errs) != 0 || len(fallbacks) != 0 || len(refs) != 1 || !strings.HasSuffix(refs[0], ".txt") {
		t.Fatalf("refs/fallbacks/errs = %v/%v/%v, want one saved text attachment", refs, fallbacks, errs)
	}
}

func TestInputTextWithMediaKeepsDeferredFailurePlaceholder(t *testing.T) {
	workspace := t.TempDir()
	adapter := newFakeAdapter(PlatformFeishu, "feishu")
	gw := NewGateway(GatewayConfig{WorkspaceRoot: workspace}, map[Platform]Adapter{PlatformFeishu: adapter}, discardLogger())
	input := gw.inputTextWithMedia(context.Background(), adapter, InboundMessage{
		Platform: PlatformFeishu,
		ChatType: ChatDM,
		ChatID:   "chat",
		Media: []InboundMedia{{
			FailureText: "[文件下载失败: report.pdf]",
			Load: func(context.Context) ([]byte, string, error) {
				return nil, "", os.ErrNotExist
			},
		}},
	}, nil)
	if input != "[文件下载失败: report.pdf]" {
		t.Fatalf("input = %q, want deferred failure placeholder", input)
	}
}
