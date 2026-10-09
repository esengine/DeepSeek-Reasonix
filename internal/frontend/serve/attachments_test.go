package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// A browser client cannot write the attachment file itself, so this endpoint is
// the one host step an image needs. It must hand back the exact "@path" token
// the turn parser resolves, or the reference silently reads as plain text.
func TestAttachmentSavesAndReturnsATurnReference(t *testing.T) {
	dir := testenv.TempDir(t)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: dir})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"mime": "image/png", "data": base64.StdEncoding.EncodeToString(buf.Bytes())})
	resp, err := http.Post(srv.URL+"/attachments", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /attachments: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /attachments = %d", resp.StatusCode)
	}
	var got struct{ Path, Ref string }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Ref != "@"+got.Path {
		t.Fatalf("ref = %q, want the path prefixed with @ (%q)", got.Ref, got.Path)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(got.Path))); err != nil {
		t.Fatalf("saved attachment is not under the workspace: %v", err)
	}
}

func attachmentServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: root})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func postAttachment(t *testing.T, srv *httptest.Server, body map[string]string) (int, Reason) {
	t.Helper()
	payload, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+"/attachments", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return resp.StatusCode, Reason{}
	}
	return resp.StatusCode, reasonOf(t, raw)
}

func b64(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A Windows .ico is the file that put a bare "add failed" in front of a user:
// the kernel said why in prose only the tooltip showed.
func TestAttachmentNamesTheTypeItRefusedAsAnImage(t *testing.T) {
	srv := attachmentServer(t, testenv.TempDir(t))
	ico := append([]byte{0, 0, 1, 0, 1, 0, 16, 16, 0, 0, 1, 0, 32, 0}, make([]byte, 64)...)
	for name, body := range map[string]map[string]string{
		"ico":           {"mime": "image/x-icon", "name": "favicon.ico", "data": b64(ico)},
		"junk as png":   {"mime": "image/png", "name": "shot.png", "data": b64([]byte("not an image"))},
		"png as x-icon": {"mime": "image/x-icon", "name": "logo.ico", "data": b64(tinyPNG(t))},
	} {
		t.Run(name, func(t *testing.T) {
			status, why := postAttachment(t, srv, body)
			if status != http.StatusUnsupportedMediaType || why.Code != "attachment.unsupported_image" {
				t.Fatalf("got %d %q, want 415 attachment.unsupported_image", status, why.Code)
			}
			if why.Params["supported"] != "PNG, JPEG, GIF, WebP" {
				t.Fatalf("supported = %v", why.Params["supported"])
			}
			if why.Params["format"] != body["name"][len(body["name"])-4:] {
				t.Fatalf("format = %v, want the extension the file was named with", why.Params["format"])
			}
		})
	}
	_, why := postAttachment(t, srv, map[string]string{"mime": "image/x-icon", "name": "favicon.ico", "data": b64(ico)})
	if why.Params["type"] == "" || why.Params["type"] == nil {
		t.Fatalf("the refusal does not say what the bytes were sniffed as: %v", why.Params)
	}
}

func TestAttachmentWithoutANameReportsTheSniffedType(t *testing.T) {
	_, why := postAttachment(t, attachmentServer(t, testenv.TempDir(t)), map[string]string{"mime": "image/png", "data": b64([]byte("<svg xmlns='http://www.w3.org/2000/svg'/>"))})
	if why.Code != "attachment.unsupported_image" || why.Params["format"] != why.Params["type"] || why.Params["format"] == "" {
		t.Fatalf("got %q %v, want the sniffed type standing in for the extension", why.Code, why.Params)
	}
}

func TestAttachmentRefusalsCarryTheirClass(t *testing.T) {
	srv := attachmentServer(t, testenv.TempDir(t))
	cases := []struct {
		name   string
		body   map[string]string
		status int
		code   string
		limit  float64
	}{
		{"empty image", map[string]string{"mime": "image/png", "data": ""}, http.StatusBadRequest, "attachment.empty", 0},
		{"empty file", map[string]string{"mime": "text/plain", "name": "a.txt", "data": ""}, http.StatusBadRequest, "attachment.empty", 0},
		{"image over 10 MB", map[string]string{"mime": "image/png", "data": b64(append(tinyPNG(t), make([]byte, 10<<20)...))}, http.StatusRequestEntityTooLarge, "attachment.too_large", 10},
		{"file over 25 MB", map[string]string{"mime": "text/plain", "name": "a.txt", "data": b64(make([]byte, 25<<20+1))}, http.StatusRequestEntityTooLarge, "attachment.too_large", 25},
		{"body over the cap", map[string]string{"mime": "text/plain", "name": "a.txt", "data": strings.Repeat("A", 40<<20)}, http.StatusRequestEntityTooLarge, "attachment.too_large", 25},
		{"not base64", map[string]string{"mime": "text/plain", "name": "a.txt", "data": "***"}, http.StatusBadRequest, "request.bad_body", 0},
		{"missing path", map[string]string{"path": filepath.Join(t.TempDir(), "gone.png")}, http.StatusBadRequest, "attachment.unreadable", 0},
		{"directory", map[string]string{"path": t.TempDir()}, http.StatusBadRequest, "attachment.unreadable", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, why := postAttachment(t, srv, c.body)
			if status != c.status || why.Code != c.code {
				t.Fatalf("got %d %q, want %d %q", status, why.Code, c.status, c.code)
			}
			if c.limit != 0 && why.Params["limit_mb"] != c.limit {
				t.Fatalf("limit_mb = %v, want %v", why.Params["limit_mb"], c.limit)
			}
		})
	}
}

func TestAttachmentSaveFailureIsOwnedByTheDisk(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, ".reasonix"), []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, why := postAttachment(t, attachmentServer(t, root), map[string]string{"mime": "text/plain", "name": "a.txt", "data": b64([]byte("hi"))})
	if status != http.StatusInternalServerError || why.Code != "attachment.write_failed" {
		t.Fatalf("got %d %q, want 500 attachment.write_failed", status, why.Code)
	}
	if why.Params["detail"] == "" || why.Params["detail"] == nil {
		t.Fatalf("the refusal drops what the disk said: %v", why.Params)
	}
}

func TestAttachmentStillStoresAPlainFile(t *testing.T) {
	status, _ := postAttachment(t, attachmentServer(t, testenv.TempDir(t)), map[string]string{"mime": "text/plain", "name": "notes.txt", "data": b64([]byte("hello"))})
	if status != http.StatusOK {
		t.Fatalf("a text file = %d, want 200", status)
	}
}
