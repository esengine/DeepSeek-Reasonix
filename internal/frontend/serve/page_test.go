package serve

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

// The page's own files answer for themselves and every other path is the
// kernel's. The split is read off the page, not off a list of kernel routes:
// such a list needs editing every time the kernel grows one.
func TestThePagesOwnFilesAnswerAndTheRestIsTheKernels(t *testing.T) {
	page := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<title>studio</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("// the page")},
	}
	kernel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"from":"the kernel"}`)
	})
	srv := httptest.NewServer(withPage(kernel, page))
	defer srv.Close()

	for _, c := range []struct{ path, want string }{
		// A real file in the page answers with itself.
		{"/assets/app.js", "// the page"},
		// Everything else is the kernel's, including the root: its own handler
		// is what serves the page shell back, and what shows a setup page
		// instead when one is owed.
		{"/", "the kernel"},
		{"/status", "the kernel"},
		// A path the page routes itself names no file, so it reaches the
		// kernel rather than being answered here.
		{"/sessions/whatever", "the kernel"},
	} {
		t.Run(c.path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + c.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), c.want) {
				t.Errorf("%s answered %q, want it to contain %q", c.path, body, c.want)
			}
		})
	}
}

// The page sits inside the auth gate. A window reaches it behind a loopback
// gate that answers first, but a serve bound to an address has only this one —
// and a page served ahead of it is one handed to whoever can reach the port.
func TestPageIsBehindTheAuthGate(t *testing.T) {
	hub := NewHub(HubOptions{
		Serve: config.ServeConfig{AuthMode: "token", Token: "the-token"},
		Page:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<title>studio</title>")}},
	})
	srv := httptest.NewServer(hub.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("the page answered %d without a credential: %q", resp.StatusCode, body)
	}
}

// An explicit directory holding no page is a launch that would open on nothing,
// and it fails where the reader can still act on it rather than at first paint.
func TestFindPageRefusesADirectoryWithNoIndex(t *testing.T) {
	if _, err := FindPage(testenv.TempDir(t)); err == nil {
		t.Fatal("a directory with no index.html was accepted as a page")
	}
	dir := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>x</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := FindPage(dir)
	if err != nil || got == nil {
		t.Fatalf("FindPage(%q) = %v, %v; want the directory", dir, got, err)
	}
}

// A type another program registered for an extension (on Windows the standard
// library reads them from the registry) must not decide what the page's own
// files are served as: an SVG that is not image/svg+xml is a broken image.
func TestThePagesFilesKeepTheirTypeWhateverTheMachineRegistered(t *testing.T) {
	for ext, bad := range map[string]string{".svg": "application/octet-stream", ".js": "text/plain", ".css": "text/plain"} {
		want := map[string]string{".svg": "image/svg+xml", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8"}[ext]
		if err := mime.AddExtensionType(ext, bad); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = mime.AddExtensionType(ext, want) })
	}
	page := fstest.MapFS{
		"assets/logo.svg": &fstest.MapFile{Data: []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")},
		"assets/app.js":   &fstest.MapFile{Data: []byte("//")},
		"assets/app.css":  &fstest.MapFile{Data: []byte("a{}")},
	}
	srv := httptest.NewServer(withPage(http.NotFoundHandler(), page))
	defer srv.Close()
	for path, want := range map[string]string{
		"/assets/logo.svg": "image/svg+xml",
		"/assets/app.js":   "text/javascript; charset=utf-8",
		"/assets/app.css":  "text/css; charset=utf-8",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Content-Type"); got != want {
			t.Errorf("%s is served as %q, want %q", path, got, want)
		}
	}
}

// A package manager that puts a link on the PATH leaves the page beside the
// real executable, not beside the link. Homebrew's cask does exactly that.
func TestPageIsFoundBesideTheRealExecutableOfALink(t *testing.T) {
	root := testenv.TempDir(t)
	real := filepath.Join(root, "Caskroom", "reasonix", "2.0.0")
	dist := filepath.Join(real, "frontend-next", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<title>studio</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(real, "reasonix")
	if err := os.WriteFile(exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "reasonix")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	page := pageBeside(link)
	if page == nil {
		t.Fatal("the page beside the real executable was not found through the link")
	}
	if _, err := fs.Stat(page, "index.html"); err != nil {
		t.Errorf("the page found has no index.html: %v", err)
	}
}

// A page beside the link itself still wins over one beside its target, so an
// install that never used a link resolves as it always did.
func TestPageBesideTheLinkComesFirst(t *testing.T) {
	root := testenv.TempDir(t)
	for _, d := range []string{"here", "there"} {
		dist := filepath.Join(root, d, "frontend-next", "dist")
		if err := os.MkdirAll(dist, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(d), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(root, "there", "reasonix")
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "here", "reasonix")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	page := pageBeside(link)
	if page == nil {
		t.Fatal("no page found")
	}
	got, err := fs.ReadFile(page, "index.html")
	if err != nil || string(got) != "here" {
		t.Errorf("found %q (%v), want the page beside the link", got, err)
	}
}

func TestNoPageBesideAnExecutableIsNil(t *testing.T) {
	if page := pageBeside(filepath.Join(testenv.TempDir(t), "reasonix")); page != nil {
		t.Errorf("found a page where none exists")
	}
}
