package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/tool"
)

func artifactContext(raw string) context.Context {
	return tool.WithResultReader(context.Background(), func(ref string) (io.ReadCloser, error) {
		if ref != "tool-result:fixture" {
			return nil, os.ErrNotExist
		}
		return io.NopCloser(strings.NewReader(raw)), nil
	})
}

func TestDownloadArtifactRetainsSourceAndDoesNotOverwrite(t *testing.T) {
	data := mp4Head()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/clip?signature=private", http.StatusFound)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	dir := t.TempDir()
	w := artifactDownload{workDir: dir, roots: realRoots([]string{dir}), proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}
	ctx := artifactContext(`{"version":1,"result":{"structuredContent":{"video_url":"` + server.URL + `/start"},"future":42}}`)
	args := json.RawMessage(`{"source":"tool-result:fixture","pointer":"/structuredContent/video_url","path":"media/clip.mp4"}`)
	out, err := w.Execute(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	var receipt artifactReceipt
	if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt.SHA256 == "" || receipt.Bytes != int64(len(data)) {
		t.Fatalf("receipt %s: %v", out, err)
	}
	if strings.Contains(out, "signature") {
		t.Fatal("signed URL leaked into model receipt")
	}
	saved, err := os.ReadFile(filepath.Join(dir, receipt.Path))
	if err != nil || string(saved) != string(data) {
		t.Fatalf("saved bytes %q: %v", saved, err)
	}
	source, err := os.ReadFile(filepath.Join(dir, receipt.ProvenancePath))
	if err != nil || strings.Contains(string(source), "signature") || !strings.Contains(string(source), "tool-result:fixture") || !strings.Contains(string(source), server.URL) {
		t.Fatalf("source receipt must name the origin without its signed query %s: %v", source, err)
	}
	if _, err := w.Execute(ctx, args); refusalCode(err) != CodeArtifactTargetExists {
		t.Fatalf("existing artifact: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, receipt.ProvenancePath)); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("private provenance permissions: %v %v", info, err)
	}
}

func TestDownloadArtifactRefusesUnobservedSourceAndPartialFiles(t *testing.T) {
	for _, tc := range []struct{ name, result, pointer, path, body string }{
		{"missing field", `{}`, "/video_url", "clip.mp4", string(mp4Head())},
		{"text is not an address field", `{"content":[{"type":"text","text":"go https://example.com"}]}`, "/content/0/text", "clip.mp4", string(mp4Head())},
		{"escape", `{"video_url":"URL"}`, "/video_url", "../clip.mp4", string(mp4Head())},
		{"playlist", `{"video_url":"URL"}`, "/video_url", "clip.mp4", "#EXTM3U\nhttp://internal/"},
		{"oversized stream", `{"video_url":"URL"}`, "/video_url", "clip.mp4", string(append(mp4Head(), make([]byte, 300)...))},
		{"future envelope", `{"video_url":"URL"}`, "/video_url", "clip.mp4", string(mp4Head())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			dir := t.TempDir()
			version := 1
			if tc.name == "future envelope" {
				version = 2
			}
			raw, _ := json.Marshal(map[string]any{"version": version, "result": json.RawMessage(strings.ReplaceAll(tc.result, "URL", server.URL))})
			args, _ := json.Marshal(map[string]any{"source": "tool-result:fixture", "pointer": tc.pointer, "path": tc.path, "max_bytes": 100})
			_, err := (artifactDownload{workDir: dir, roots: realRoots([]string{dir}), proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}).Execute(artifactContext(string(raw)), args)
			if err == nil {
				t.Fatal("invalid import accepted")
			}
			_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err == nil && !entry.IsDir() {
					t.Errorf("partial file left: %s", path)
				}
				return err
			})
		})
	}
}

func TestArtifactSourceSelectsExactFieldsWithoutURLRewriting(t *testing.T) {
	for _, tc := range []struct {
		name, result, pointer, want string
	}{
		{"escaped key", `{"a/b":{"~url":"https://example.test/v?signature=private"}}`, "/a~1b/~0url", "https://example.test/v?signature=private"},
		{"resource", `{"content":[{"type":"resource_link","uri":"https://example.test/v"}]}`, "/content/0/uri", "https://example.test/v"},
		{"fragment", `{"url":"https://example.test/v#fragment"}`, "/url", ""},
		{"credentials", `{"url":"https://user:secret@example.test/v"}`, "/url", ""},
		{"invalid escape", `{"url":"https://example.test/v"}`, "/~2url", ""},
		{"failed result", `{"isError":true,"url":"https://example.test/v"}`, "/url", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := artifactContext(`{"version":1,"result":` + tc.result + `}`)
			got, err := artifactSourceURL(ctx, "tool-result:fixture", tc.pointer)
			if tc.want == "" {
				if err == nil || refusalCode(err) != CodeArtifactSource {
					t.Fatalf("accepted invalid source: %v %v", got, err)
				}
				return
			}
			if err != nil || got.String() != tc.want {
				t.Fatalf("address changed: %v %v", got, err)
			}
		})
	}
}

func TestDownloadArtifactTransportFailureRedactsSignedAddress(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/creds" {
			http.Redirect(w, r, "http://user:private-password@127.0.0.1/video", http.StatusFound)
			return
		}
		http.Redirect(w, r, deadURL+"/video?signature=private-hop", http.StatusFound)
	}))
	defer server.Close()
	w := artifactDownload{proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}
	_, _, err := w.fetch(context.Background(), server.URL+"/creds?signature=private-token")
	if refusalCode(err) != CodeArtifactRedirectRefused || strings.Contains(err.Error(), "private-") {
		t.Fatalf("credentialed redirect: %v", err)
	}
	_, _, err = w.fetch(context.Background(), server.URL+"/dead?signature=private-token")
	var cause *url.Error
	if err == nil || !errors.As(err, &cause) {
		t.Fatalf("missing transport cause: %v", err)
	}
	if strings.Contains(err.Error(), "private-") {
		t.Fatalf("signed URL leaked: %v", err)
	}
}

func TestDownloadArtifactRefusesLongRedirectChains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	defer server.Close()
	w := artifactDownload{proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}
	_, _, err := w.fetch(context.Background(), server.URL+"/v")
	if refusalCode(err) != CodeArtifactRedirectRefused {
		t.Fatalf("redirect chain past the cap: %v", err)
	}
}

func TestDownloadArtifactFollowsCrossOriginRedirectWithoutCredentials(t *testing.T) {
	var gotAuth, gotCookie string
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotCookie = r.Header.Get("Authorization"), r.Header.Get("Cookie")
		_, _ = w.Write(mp4Head())
	}))
	defer cdn.Close()
	cdnURL := strings.Replace(cdn.URL, "127.0.0.1", "localhost", 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdnURL+"/clip?sig=private", http.StatusFound)
	}))
	defer origin.Close()
	w := artifactDownload{proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}
	resp, hosts, err := w.fetch(context.Background(), origin.URL+"/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth != "" || gotCookie != "" {
		t.Fatalf("credentials crossed origins: %q %q", gotAuth, gotCookie)
	}
	if len(hosts) != 1 || strings.Contains(hosts[0], "sig") {
		t.Fatalf("redirect hosts %v", hosts)
	}
}
