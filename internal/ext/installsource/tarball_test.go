package installsource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

type tarEntry struct {
	name     string
	body     string
	typeflag byte
	mode     int64
	linkname string
}

func buildTarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		flag := e.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		if flag == tar.TypeXGlobalHeader {
			// The writer accepts nothing but PAXRecords on a global header.
			if err := tw.WriteHeader(&tar.Header{Name: e.name, Typeflag: flag, PAXRecords: map[string]string{"comment": e.body}}); err != nil {
				t.Fatalf("write pax header: %v", err)
			}
			continue
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		head := &tar.Header{Name: e.name, Typeflag: flag, Mode: mode, Size: int64(len(e.body)), Linkname: e.linkname}
		if flag == tar.TypeDir {
			head.Size = 0
		}
		if err := tw.WriteHeader(head); err != nil {
			t.Fatalf("write header %s: %v", e.name, err)
		}
		if flag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body %s: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// The unpacked tree has to match what a clone leaves behind: the archive's own
// root directory is dropped, and a symlink is left out rather than recreated.
func TestUnpackTarballStripsRootAndOmitsSymlinks(t *testing.T) {
	dir := testenv.TempDir(t)
	archive := buildTarball(t, []tarEntry{
		{name: "bar-abc/", typeflag: tar.TypeDir},
		{name: "bar-abc/plugin.json", body: `{"name":"bar"}`},
		{name: "bar-abc/hooks/", typeflag: tar.TypeDir},
		{name: "bar-abc/hooks/run.sh", body: "#!/bin/sh\n", mode: 0o755},
		{name: "bar-abc/escape", typeflag: tar.TypeSymlink, linkname: "../../../etc/passwd"},
	})

	root, err := unpackTarball(bytes.NewReader(archive), dir)
	if err != nil {
		t.Fatalf("unpackTarball: %v", err)
	}
	if root != "bar-abc" {
		t.Fatalf("root = %q, want bar-abc", root)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "plugin.json")); err != nil || string(body) != `{"name":"bar"}` {
		t.Fatalf("plugin.json = %q, %v", body, err)
	}
	hook, err := os.Stat(filepath.Join(dir, "hooks", "run.sh"))
	if err != nil {
		t.Fatalf("hook missing: %v", err)
	}
	// Windows carries no executable bit — os.Chmod only toggles read-only there —
	// so the mode a tarball declared cannot survive an unpack onto it. The claim
	// is checkable only where the bit means something.
	if runtime.GOOS != "windows" && hook.Mode().Perm() != 0o755 {
		t.Fatalf("hook mode = %v, want 0755 so it stays runnable", hook.Mode().Perm())
	}
	if _, err := os.Lstat(filepath.Join(dir, "escape")); !os.IsNotExist(err) {
		t.Fatalf("symlink was recreated (err = %v); a link may point outside the tree", err)
	}
}

// A path that climbs out of the extraction directory is refused outright rather
// than silently skipped: it means the archive is not what it claims to be.
func TestUnpackTarballRejectsEscapingPath(t *testing.T) {
	dir := testenv.TempDir(t)
	archive := buildTarball(t, []tarEntry{
		{name: "bar-abc/", typeflag: tar.TypeDir},
		{name: "bar-abc/../../evil.sh", body: "payload"},
	})

	if _, err := unpackTarball(bytes.NewReader(archive), dir); err == nil {
		t.Fatal("escaping entry accepted")
	} else if !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error = %v, want it to name the escape", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.sh")); !os.IsNotExist(err) {
		t.Fatalf("escaping file landed outside the directory: %v", err)
	}
}

// A header that understates its body must not let the stream write past the
// per-entry cap, so the limit is enforced on what is copied.
func TestUnpackTarballBoundsALyingHeader(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := strings.Repeat("x", tarballEntryLimit+4096)
	// Declare the real size so the writer accepts it; the cap is what matters.
	_ = tw.WriteHeader(&tar.Header{Name: "bar-abc/big", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))})
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()

	if _, err := unpackTarball(bytes.NewReader(buf.Bytes()), testenv.TempDir(t)); err == nil {
		t.Fatal("oversized entry accepted")
	} else if !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("error = %v, want the size cap", err)
	}
}

// The commit is what the approval contract pins, and for a tarball it can only
// come from the archive root — so a root that names one must yield it, and a tag
// archive (no object name) must yield empty rather than a guess.
func TestFetchGitHubTarballReadsCommitFromRoot(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for _, tc := range []struct{ root, want string }{
		{"bar-" + sha, sha},
		{"bar-1.0", ""},
	} {
		t.Run(tc.root, func(t *testing.T) {
			archive := buildTarball(t, []tarEntry{
				{name: tc.root + "/", typeflag: tar.TypeDir},
				{name: tc.root + "/plugin.json", body: "{}"},
			})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/foo/bar/tarball" {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write(archive)
			}))
			defer srv.Close()
			old := githubAPIBaseURL
			githubAPIBaseURL = srv.URL
			defer func() { githubAPIBaseURL = old }()

			tl := &Tool{httpClient: srv.Client()}
			dir := testenv.TempDir(t)
			commit, err := tl.fetchGitHubTarball(context.Background(), githubRepoSource{Owner: "foo", Repo: "bar"}, dir)
			if err != nil {
				t.Fatalf("fetchGitHubTarball: %v", err)
			}
			if commit != tc.want {
				t.Fatalf("commit = %q, want %q", commit, tc.want)
			}
			if _, err := os.Stat(filepath.Join(dir, "plugin.json")); err != nil {
				t.Fatalf("tree not unpacked: %v", err)
			}
		})
	}
}

// A branch rides the URL, because that is the only way the tarball endpoint can
// serve anything but the default branch.
func TestFetchGitHubTarballRequestsTheBranch(t *testing.T) {
	archive := buildTarball(t, []tarEntry{{name: "bar-abc/", typeflag: tar.TypeDir}, {name: "bar-abc/plugin.json", body: "{}"}})
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	old := githubAPIBaseURL
	githubAPIBaseURL = srv.URL
	defer func() { githubAPIBaseURL = old }()

	tl := &Tool{httpClient: srv.Client()}
	if _, err := tl.fetchGitHubTarball(context.Background(), githubRepoSource{Owner: "foo", Repo: "bar", Branch: "next"}, testenv.TempDir(t)); err != nil {
		t.Fatalf("fetchGitHubTarball: %v", err)
	}
	if want := "/repos/foo/bar/tarball/next"; got != want {
		t.Fatalf("requested %q, want %q", got, want)
	}
}

// An unreachable archive has to arrive as the tool's own unreadable-source
// error, not as a bare transport failure the caller cannot classify.
func TestFetchGitHubTarballClassifiesFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	old := githubAPIBaseURL
	githubAPIBaseURL = srv.URL
	defer func() { githubAPIBaseURL = old }()

	tl := &Tool{httpClient: srv.Client()}
	_, err := tl.fetchGitHubTarball(context.Background(), githubRepoSource{Owner: "foo", Repo: "bar"}, testenv.TempDir(t))
	if err == nil {
		t.Fatal("missing archive accepted")
	}
	if !strings.Contains(fmt.Sprint(err), "HTTP 404") {
		t.Fatalf("error = %v, want it to carry the status", err)
	}
}

// GitHub puts a pax_global_header entry first. It is not the archive root, and
// treating it as one strips every real path down to nothing.
func TestUnpackTarballIgnoresPaxGlobalHeader(t *testing.T) {
	dir := testenv.TempDir(t)
	archive := buildTarball(t, []tarEntry{
		{name: "pax_global_header", body: "52 comment=abc\n", typeflag: tar.TypeXGlobalHeader},
		{name: "bar-abc/", typeflag: tar.TypeDir},
		{name: "bar-abc/plugin.json", body: "{}"},
	})

	root, err := unpackTarball(bytes.NewReader(archive), dir)
	if err != nil {
		t.Fatalf("unpackTarball: %v", err)
	}
	if root != "bar-abc" {
		t.Fatalf("root = %q, want bar-abc — the pax header is not the root", root)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugin.json")); err != nil {
		t.Fatalf("tree not unpacked: %v", err)
	}
}
