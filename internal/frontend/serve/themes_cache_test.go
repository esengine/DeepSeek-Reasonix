package serve

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/theme"
)

func TestReimportedThemeAssetsRequireFreshReads(t *testing.T) {
	for _, asset := range []string{"background", "preview"} {
		t.Run(asset, func(t *testing.T) {
			t.Setenv("REASONIX_HOME", testenv.TempDir(t))
			srv := themeServer(t)
			url := srv.URL + "/themes/dusk/" + asset
			var policies []string
			previousTag := ""
			for _, ink := range []color.NRGBA{{R: 255, A: 255}, {B: 255, A: 255}} {
				img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
				img.SetNRGBA(0, 0, ink)
				var body bytes.Buffer
				if err := png.Encode(&body, img); err != nil {
					t.Fatal(err)
				}
				resp := postJSON(t, srv.URL+"/themes/import", map[string]any{"files": map[string]string{
					"theme.json":   base64.StdEncoding.EncodeToString([]byte(importedPack)),
					asset + ".png": base64.StdEncoding.EncodeToString(body.Bytes()),
				}})
				var got theme.Installed
				err := json.NewDecoder(resp.Body).Decode(&got)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK || err != nil || got.Pack.ID != "dusk" {
					t.Fatalf("import = %d, id=%q, err=%v", resp.StatusCode, got.Pack.ID, err)
				}
				request, err := http.NewRequest(http.MethodGet, url, nil)
				if err != nil {
					t.Fatal(err)
				}
				if previousTag != "" {
					request.Header.Set("If-None-Match", previousTag)
				}
				resp, err = http.DefaultClient.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				read, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK || err != nil || !bytes.Equal(read, body.Bytes()) {
					t.Fatalf("asset = %d, bytes=%d, err=%v; want the imported image", resp.StatusCode, len(read), err)
				}
				if got := resp.Header.Get("Content-Type"); got != "image/png" {
					t.Fatalf("content type = %q", got)
				}
				tag := resp.Header.Get("ETag")
				if want := fmt.Sprintf(`"%x"`, sha256.Sum256(body.Bytes())); tag != want || tag == previousTag {
					t.Fatalf("ETag = %q, previous=%q, want current content hash %q", tag, previousTag, want)
				}
				policies = append(policies, resp.Header.Get("Cache-Control"))
				for _, match := range []string{tag, "W/" + tag, `"another-asset", ` + tag} {
					request, err := http.NewRequest(http.MethodGet, url, nil)
					if err != nil {
						t.Fatal(err)
					}
					request.Header.Set("If-None-Match", match)
					cached, err := http.DefaultClient.Do(request)
					if err != nil {
						t.Fatal(err)
					}
					read, err := io.ReadAll(cached.Body)
					cached.Body.Close()
					if cached.StatusCode != http.StatusNotModified || err != nil || len(read) != 0 || cached.Header.Get("ETag") != tag || cached.Header.Get("Cache-Control") != "private, no-cache" {
						t.Fatalf("unchanged asset match %q = %d, bytes=%d, headers=%v, err=%v", match, cached.StatusCode, len(read), cached.Header, err)
					}
				}
				request, err = http.NewRequest(http.MethodHead, url, nil)
				if err != nil {
					t.Fatal(err)
				}
				head, err := http.DefaultClient.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				read, err = io.ReadAll(head.Body)
				head.Body.Close()
				if head.StatusCode != http.StatusOK || err != nil || len(read) != 0 || head.ContentLength != int64(body.Len()) || head.Header.Get("ETag") != tag || head.Header.Get("Content-Type") != "image/png" {
					t.Fatalf("HEAD = %d, bytes=%d, length=%d, headers=%v, err=%v", head.StatusCode, len(read), head.ContentLength, head.Header, err)
				}
				previousTag = tag
			}
			for _, policy := range policies {
				if policy != "private, no-cache" {
					t.Fatalf("replaced image at %s may be reused without validation: %q", url, policy)
				}
			}
		})
	}
}
