package serve

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestAttachmentStreamAcceptsLargeAPK(t *testing.T) {
	root := t.TempDir()
	srv := attachmentServer(t, root)
	const size = 65 << 20
	resp, err := http.Post(srv.URL+"/attachments/upload?name=large.apk", "application/octet-stream", io.LimitReader(repeatingByte{}, size))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	var got struct {
		Path, Ref string
		Image     bool
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, got.Path))
	if err != nil || info.Size() != size || got.Ref != "@"+got.Path || got.Image {
		t.Fatalf("result %+v, stat %v, err %v", got, info, err)
	}
}

type repeatingByte struct{}

func (repeatingByte) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestAttachmentStreamRejectsSimpleContentType(t *testing.T) {
	srv := attachmentServer(t, t.TempDir())
	resp, err := http.Post(srv.URL+"/attachments/upload?name=a.apk", "text/plain", bytes.NewReader([]byte("APK")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestAttachmentStreamRetainsImageLimit(t *testing.T) {
	srv := attachmentServer(t, t.TempDir())
	resp, err := http.Post(srv.URL+"/attachments/upload?name=photo.png&mime=image/png", "application/octet-stream", io.LimitReader(repeatingByte{}, 11<<20))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var why Reason
	if err := json.NewDecoder(resp.Body).Decode(&why); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusRequestEntityTooLarge || why.Code != "attachment.too_large" {
		t.Fatalf("status %d, reason %+v", resp.StatusCode, why)
	}
}
