package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestAttachmentRefusesWhatIsNotAnImage(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: testenv.TempDir(t)})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	junk, _ := json.Marshal(map[string]string{"mime": "image/png", "data": base64.StdEncoding.EncodeToString([]byte("not an image"))})
	resp, err := http.Post(srv.URL+"/attachments", "application/json", bytes.NewReader(junk))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /attachments with junk = %d, want 400", resp.StatusCode)
	}
}
