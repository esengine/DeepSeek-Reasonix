package serve

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestWorkspaceVideoServesRangesOfAVideoInTheTree(t *testing.T) {
	clip := "\x00\x00\x00\x18ftypisom" + strings.Repeat("v", 100)
	srv := imageServer(t, map[string]string{"video/teaser/s01-v1.mp4": clip})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/workspace/video?path=video/teaser/s01-v1.mp4", nil)
	req.Header.Set("Range", "bytes=4-11")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Type") != "video/mp4" || string(body) != "ftypisom" {
		t.Fatalf("status %d, type %q, body %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("video served without the sandbox headers: %q", csp)
	}
}

func TestWorkspaceVideoRefusesWhatIsNotAVideoInTheTree(t *testing.T) {
	srv := imageServer(t, map[string]string{"page.html": "<script>1</script>", "fake.mp4": "<html><script>1</script>", "a.webm": "\x1a\x45\xdf\xa3webm"})
	for path, want := range map[string]int{
		"page.html":      http.StatusBadRequest,
		"fake.mp4":       http.StatusBadRequest,
		"../outside.mp4": http.StatusBadRequest,
		"missing.mp4":    http.StatusNotFound,
		"a.webm":         http.StatusOK,
	} {
		resp, err := http.Get(srv.URL + "/workspace/video?path=" + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: status %d, want %d", path, resp.StatusCode, want)
		}
	}
}
