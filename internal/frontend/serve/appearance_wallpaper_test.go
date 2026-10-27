package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func wallpaperMux() *http.ServeMux {
	mux := http.NewServeMux()
	(&Server{}).registerAppearanceRoutes(mux)
	return mux
}

func uploadWallpaperBody(t *testing.T, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	wallpaperMux().ServeHTTP(rec, req)
	return rec
}

func uploadedName(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var view struct {
		Wallpaper *struct {
			URL string `json:"url"`
		} `json:"wallpaper"`
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil || view.Wallpaper == nil {
		t.Fatalf("upload answered %s: %v", rec.Body.String(), err)
	}
	return path.Base(view.Wallpaper.URL)
}

func TestWallpaperUploadIsRawAndHasNoSizeLimit(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	// Past the 12 MiB the old JSON path stopped at.
	body := bytes.Repeat([]byte{7}, 13<<20)
	name := uploadedName(t, uploadWallpaperBody(t, "/appearance/wallpaper?mime=image/png", bytes.NewReader(body)))
	stored, err := os.ReadFile(filepath.Join(appearanceDir(), name))
	if err != nil || !bytes.Equal(stored, body) {
		t.Fatalf("stored wallpaper differs from the upload: %v", err)
	}
}

func TestWallpaperUploadRefusalsKeepTheirCodes(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	for _, c := range []struct{ target, body, code string }{
		{"/appearance/wallpaper?mime=application/pdf", "x", "wallpaper.unsupported_type"},
		{"/appearance/wallpaper", "x", "wallpaper.unsupported_type"},
		{"/appearance/wallpaper?mime=image/png", "", "wallpaper.empty"},
	} {
		rec := uploadWallpaperBody(t, c.target, strings.NewReader(c.body))
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%s: %d %s, want %s", c.target, rec.Code, rec.Body.String(), c.code)
		}
	}
}

type failingReader struct{ after int }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.after <= 0 {
		return 0, errors.New("connection reset")
	}
	n := min(len(p), f.after)
	for i := range p[:n] {
		p[i] = 1
	}
	f.after -= n
	return n, nil
}

func TestAbortedWallpaperUploadLeavesNothingBehind(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	rec := uploadWallpaperBody(t, "/appearance/wallpaper?mime=image/png", &failingReader{after: 1 << 20})
	if rec.Code < 400 {
		t.Fatalf("an upload that broke off answered %d", rec.Code)
	}
	entries, _ := os.ReadDir(appearanceDir())
	if len(entries) != 0 {
		t.Fatalf("a partial wallpaper stayed behind: %v", entries)
	}
}

func TestWallpaperIsServedInRanges(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	body := []byte("0123456789abcdef")
	name := uploadedName(t, uploadWallpaperBody(t, "/appearance/wallpaper?mime=image/png", bytes.NewReader(body)))
	req := httptest.NewRequest(http.MethodGet, "/appearance/wallpaper/"+name, nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	wallpaperMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" || rec.Header().Get("Content-Range") != "bytes 2-5/16" {
		t.Fatalf("ranged read = %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Range"))
	}
	if rec.Header().Get("Content-Type") != "image/png" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("headers changed: %v", rec.Header())
	}
}

func TestCSRFGuardAdmitsBinaryWallpaperOnlyAtItsRoute(t *testing.T) {
	reached := false
	guarded := csrfGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	for target, want := range map[string]bool{"/appearance/wallpaper?mime=image/png": true, "/appearance/zoom": false, "/runtimes/x/close": false} {
		reached = false
		req := httptest.NewRequest(http.MethodPost, target, nil)
		req.Header.Set("Content-Type", "application/octet-stream")
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		if reached != want {
			t.Errorf("%s: reached = %v, want %v (status %d)", target, reached, want, rec.Code)
		}
	}
}

func TestVideoWallpapersAreAdmittedAndNamedAsVideo(t *testing.T) {
	for _, c := range []struct{ mime, ext, kind string }{
		{"video/mp4", ".mp4", "video"},
		{"video/webm", ".webm", "video"},
		{"image/webp", ".webp", "image"},
	} {
		t.Setenv("REASONIX_HOME", testenv.TempDir(t))
		rec := uploadWallpaperBody(t, "/appearance/wallpaper?mime="+c.mime, strings.NewReader("a stand-in for "+c.mime))
		var view struct {
			Wallpaper *struct {
				URL  string `json:"url"`
				Kind string `json:"kind"`
			} `json:"wallpaper"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &view) != nil || view.Wallpaper == nil {
			t.Fatalf("%s: %d %s", c.mime, rec.Code, rec.Body.String())
		}
		if !strings.HasSuffix(view.Wallpaper.URL, c.ext) || view.Wallpaper.Kind != c.kind {
			t.Errorf("%s stored as %s kind %q, want %s kind %q", c.mime, view.Wallpaper.URL, view.Wallpaper.Kind, c.ext, c.kind)
		}
		get := httptest.NewRecorder()
		wallpaperMux().ServeHTTP(get, httptest.NewRequest(http.MethodGet, view.Wallpaper.URL, nil))
		if get.Code != http.StatusOK || get.Header().Get("Content-Type") != c.mime {
			t.Errorf("%s served as %d %q", c.mime, get.Code, get.Header().Get("Content-Type"))
		}
	}
}
