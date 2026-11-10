package builtin

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestWebImageAcquisition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/photo.png" {
			w.Header().Set("Content-Type", "image/png")
			_ = png.Encode(w, image.NewRGBA(image.Rect(0, 0, 2, 2)))
			return
		}
		if r.URL.Path == "/bad.png" {
			_, _ = w.Write([]byte("not an image"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><meta property="og:image" content="/photo.png"><img src="/bad.png" alt="broken"><img src="javascript:alert(1)"></html>`))
	}))
	defer server.Close()
	out := runTool(t, webFetch{}, map[string]any{"url": server.URL})
	if !strings.Contains(out, server.URL+"/photo.png") {
		t.Errorf("web_fetch discarded image candidates: %s", out)
	}
	dir := testenv.TempDir(t)
	tools := (Workspace{Dir: dir}).Tools("import_web_image")
	if len(tools) != 1 {
		t.Fatal("image importer not registered")
	}
	importer := tools[0]
	if importer.ReadOnly() {
		t.Fatal("image importer must declare writes")
	}
	args := map[string]any{"url": server.URL, "image_url": server.URL + "/photo.png", "path": "images/photo.png", "alt": "Real source photo"}
	out = runTool(t, importer, args)
	var result struct {
		Image struct{ Path, Alt, Source string }
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Image.Path != "images/photo.png" || strings.TrimSuffix(result.Image.Source, "/") != server.URL {
		t.Fatalf("missing provenance: %s", out)
	}
	f, err := os.Open(filepath.Join(dir, "images/photo.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = png.Decode(f); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, key, value string }{
		{"overwrite", "path", "images/photo.png"},
		{"outside", "path", "../outside.png"},
		{"unobserved", "image_url", server.URL + "/unknown.png"},
		{"invalid image", "image_url", server.URL + "/bad.png"},
		{"wrong extension", "path", "images/photo.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := map[string]any{"url": server.URL, "image_url": server.URL + "/photo.png", "path": "images/new.png", "alt": "Photo"}
			p[tc.key] = tc.value
			if _, err := importer.Execute(context.Background(), argsJSON(t, p)); err == nil {
				t.Fatal("accepted invalid import")
			}
		})
	}
}

func TestWebImageLive(t *testing.T) {
	page := os.Getenv("REASONIX_IMAGE_SMOKE_PAGE")
	if page == "" {
		t.Skip("set REASONIX_IMAGE_SMOKE_PAGE for an explicit network smoke test")
	}
	w := webImage{workDir: testenv.TempDir(t)}
	body, source, err := w.fetch(context.Background(), page, webFetchMaxRead)
	if err != nil {
		t.Fatal(err)
	}
	candidates := pageImages(body, source)
	for _, candidate := range candidates {
		t.Logf("candidate: %s", candidate.URL)
	}
	selected := os.Getenv("REASONIX_IMAGE_SMOKE_URL")
	if selected == "" {
		return
	}
	out := runTool(t, w, map[string]any{"url": page, "image_url": selected, "path": "photo.jpg", "alt": "Source photo"})
	t.Log(out)
}

func TestWebImageBoundaries(t *testing.T) {
	dir := testenv.TempDir(t)
	w := webImage{workDir: dir, roots: realRoots([]string{filepath.Join(dir, "allowed")}), guard: NewSessionDataGuard(dir, nil)}
	for _, path := range []string{"outside.png", "sessions/photo.png"} {
		if _, err := w.Execute(context.Background(), argsJSON(t, map[string]any{"path": path, "alt": "Photo"})); err == nil {
			t.Fatalf("accepted forbidden path %s", path)
		}
	}
	outside := testenv.TempDir(t)
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err == nil {
		w.roots = realRoots([]string{dir})
		if _, err := w.Execute(context.Background(), argsJSON(t, map[string]any{"path": "escape/photo.png", "alt": "Photo"})); err == nil {
			t.Fatal("accepted escaping symlink")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			rw.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = rw.Write([]byte("12345"))
	}))
	defer server.Close()
	for _, raw := range []string{server.URL + "/status", server.URL, "file:///tmp/photo.png", "https://user:pass@example.com/photo"} {
		if _, _, err := w.fetch(context.Background(), raw, 4); err == nil {
			t.Fatalf("accepted invalid fetch %s", raw)
		}
	}
}

func TestPageImagesLazySourcesAndBase(t *testing.T) {
	page, _ := url.Parse("https://shop.example/catalog/item")
	for _, tc := range []struct {
		name, html string
		want       []imageCandidate
	}{
		{"lazy placeholder", `<img src="data:image/gif;base64,AA" data-src="photo.jpg" alt="Camera">`, []imageCandidate{{URL: "https://shop.example/catalog/photo.jpg", Alt: "Camera"}}},
		{"retain both", `<img src="small.jpg" data-src="large.jpg" alt="Camera">`, []imageCandidate{{URL: "https://shop.example/catalog/small.jpg", Alt: "Camera"}, {URL: "https://shop.example/catalog/large.jpg", Alt: "Camera"}}},
		{"base", `<base href="https://cdn.example/photos/"><img src="a.jpg"><base href="https://ignored.example/"><img data-src="b.jpg">`, []imageCandidate{{URL: "https://cdn.example/photos/a.jpg"}, {URL: "https://cdn.example/photos/b.jpg"}}},
		{"invalid base", `<base href="javascript:alert(1)"><base href="https://ignored.example/"><img src="a.jpg">`, []imageCandidate{{URL: "https://shop.example/catalog/a.jpg"}}},
		{"deduplicate", `<img src="a.jpg" data-src="a.jpg"><img src="a.jpg#fragment">`, []imageCandidate{{URL: "https://shop.example/catalog/a.jpg"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pageImages([]byte(tc.html), page); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v; want %#v", got, tc.want)
			}
		})
	}
}
