package bot

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareOutboundMediaReadsDataAndHashesIt(t *testing.T) {
	prepared, err := PrepareOutboundMedia(context.Background(), OutboundMedia{Kind: "file", Name: "x.txt", Data: []byte("png")}, MediaPolicy{MaxBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Size != 3 || prepared.SHA256 == "" || string(prepared.Data) != "png" {
		t.Fatalf("prepared=%+v", prepared)
	}
}

func TestPrepareOutboundMediaRejectsForgedMIME(t *testing.T) {
	_, err := PrepareOutboundMedia(context.Background(), OutboundMedia{Kind: "image", MIME: "image/png", Data: []byte("not an image")}, MediaPolicy{MaxBytes: 100})
	if err == nil || !strings.Contains(err.Error(), "MIME") {
		t.Fatalf("forged image MIME was accepted: %v", err)
	}
}

func TestPrepareOutboundMediaReadsSymlinkThroughRootWithoutEscape(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside.txt")
	if err := os.WriteFile(inside, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	insideLink := filepath.Join(root, "inside-link.txt")
	if err := os.Symlink(filepath.Base(inside), insideLink); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareOutboundMedia(context.Background(), OutboundMedia{Kind: "file", Path: insideLink}, MediaPolicy{LocalRoots: []string{root}, MaxBytes: 100})
	if err != nil || string(prepared.Data) != "inside" {
		t.Fatalf("in-root symlink = %+v, %v", prepared, err)
	}
	out := filepath.Join(filepath.Dir(root), "outside-link-target.txt")
	if err := os.WriteFile(out, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape.txt")
	if err := os.Symlink(out, escape); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareOutboundMedia(context.Background(), OutboundMedia{Kind: "file", Path: escape}, MediaPolicy{LocalRoots: []string{root}, MaxBytes: 100}); err == nil {
		t.Fatal("symlink escape was accepted")
	}
}

func TestPinnedMediaTransportRejectsPrivateDialResolution(t *testing.T) {
	oldLookup := mediaLookupIP
	mediaLookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	defer func() { mediaLookupIP = oldLookup }()
	_, err := pinnedMediaTransport(MediaPolicy{}).DialContext(context.Background(), "tcp", "cdn.example:443")
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("private dial resolution was accepted: %v", err)
	}
}

func TestPrepareOutboundMediaRejectsPrivateRedirectTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	}))
	defer server.Close()
	if _, err := PrepareOutboundMedia(context.Background(), OutboundMedia{Kind: "file", URL: server.URL}, MediaPolicy{MaxBytes: 100, ResolveDNS: true}); err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("redirect SSRF was not rejected: %v", err)
	}
}

func TestValidateOutboundMediaRejectsPrivateURLAndPathEscape(t *testing.T) {
	if err := ValidateOutboundMedia(OutboundMedia{Kind: "image", URL: "http://127.0.0.1/a"}, MediaPolicy{}); err == nil {
		t.Fatal("private media URL accepted")
	}
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside.bin")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOutboundMedia(OutboundMedia{Kind: "file", Path: outside}, MediaPolicy{LocalRoots: []string{root}}); err == nil {
		t.Fatal("media path outside allowlist accepted")
	}
}
