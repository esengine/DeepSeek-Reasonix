package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

type entry struct {
	name string
	body string
	typ  byte
	link string
}

func tarGz(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Linkname: e.link}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipOf(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.typ == tar.TypeSymlink {
			h.SetMode(os.ModeSymlink | 0o777)
		} else {
			h.SetMode(0o644)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var goodPage = []entry{
	{name: "reasonix", body: "new-binary"},
	{name: "frontend-next/dist/index.html", body: "new-index"},
	{name: "frontend-next/dist/assets/app.js", body: "new-app"},
}

func TestExtractPageReadsTheTreeFromBothArchiveKinds(t *testing.T) {
	for name, data := range map[string][]byte{"reasonix-linux-amd64.tar.gz": tarGz(t, goodPage...), "reasonix-windows-amd64.zip": zipOf(t, goodPage...)} {
		tree, err := extractPage(data, name)
		if err != nil || tree == nil {
			t.Fatalf("%s: tree %v err %v", name, tree, err)
		}
		got := map[string]string{}
		for _, f := range tree.files {
			got[f.rel] = string(f.data)
		}
		if got["index.html"] != "new-index" || got["assets/app.js"] != "new-app" || len(got) != 2 {
			t.Errorf("%s: unexpected files %v", name, got)
		}
	}
}

func TestExtractPageIsNilForAnArchiveWithoutAPage(t *testing.T) {
	tree, err := extractPage(tarGz(t, entry{name: "reasonix", body: "x"}), "reasonix-linux-amd64.tar.gz")
	if err != nil || tree != nil {
		t.Fatalf("tree %v err %v, want nil nil", tree, err)
	}
}

func TestExtractPageRefusesWhatCouldWriteOutsideThePage(t *testing.T) {
	page := func(more ...entry) []entry {
		return append([]entry{{name: "frontend-next/dist/index.html", body: "i"}}, more...)
	}
	for name, entries := range map[string][]entry{
		"parent traversal":     page(entry{name: "frontend-next/dist/../../evil", body: "x"}),
		"traversal in middle":  page(entry{name: "frontend-next/dist/a/../../../evil", body: "x"}),
		"absolute path":        {{name: "/frontend-next/dist/index.html", body: "x"}},
		"windows separator":    page(entry{name: `frontend-next/dist/a\..\b`, body: "x"}),
		"drive letter":         page(entry{name: "frontend-next/dist/C:/x", body: "x"}),
		"symlink":              page(entry{name: "frontend-next/dist/link", typ: tar.TypeSymlink, link: "/etc/passwd"}),
		"hard link":            page(entry{name: "frontend-next/dist/hard", typ: tar.TypeLink, link: "frontend-next/dist/index.html"}),
		"device":               page(entry{name: "frontend-next/dist/dev", typ: tar.TypeChar}),
		"fifo":                 page(entry{name: "frontend-next/dist/fifo", typ: tar.TypeFifo}),
		"duplicate":            page(entry{name: "frontend-next/dist/index.html", body: "again"}),
		"no index":             {{name: "frontend-next/dist/other.html", body: "x"}},
		"case-only duplicate":  page(entry{name: "frontend-next/dist/INDEX.HTML", body: "x"}),
		"file then directory":  page(entry{name: "frontend-next/dist/a", body: "x"}, entry{name: "frontend-next/dist/a/b", body: "y"}),
		"directory then file":  page(entry{name: "frontend-next/dist/a/b", body: "y"}, entry{name: "frontend-next/dist/a", body: "x"}),
		"trailing dot":         page(entry{name: "frontend-next/dist/assets/app.js.", body: "x"}),
		"trailing space":       page(entry{name: "frontend-next/dist/assets/app.js ", body: "x"}),
		"link beside the dist": {{name: "frontend-next/dist/index.html", body: "i"}, {name: "frontend-next/other", typ: tar.TypeSymlink, link: "/"}},
	} {
		t.Run(name, func(t *testing.T) {
			if tree, err := extractPage(tarGz(t, entries...), "reasonix-linux-amd64.tar.gz"); err == nil {
				t.Fatalf("accepted: %v", tree)
			}
		})
	}
	if _, err := extractPage(zipOf(t, entry{name: "frontend-next/dist/index.html", body: "i"}, entry{name: "frontend-next/dist/link", typ: tar.TypeSymlink, body: "/etc"}), "reasonix-windows-amd64.zip"); err == nil {
		t.Error("a zip symlink was accepted")
	}
	if _, err := extractPage(zipOf(t, entry{name: "frontend-next/dist/index.html", body: "i"}, entry{name: "frontend-next/dist/../../evil", body: "x"}), "reasonix-windows-amd64.zip"); err == nil {
		t.Error("a zip traversal was accepted")
	}
}

func TestExtractPageIsBounded(t *testing.T) {
	many := []entry{{name: "frontend-next/dist/index.html", body: "i"}}
	for i := 0; i <= maxPageEntries; i++ {
		many = append(many, entry{name: fmt.Sprintf("frontend-next/dist/f%d.txt", i), body: "x"})
	}
	if _, err := extractPage(tarGz(t, many...), "reasonix-linux-amd64.tar.gz"); err == nil {
		t.Error("an archive with too many entries was accepted")
	}
	chunk := strings.Repeat("b", 48<<20)
	if _, err := extractPage(tarGz(t, entry{name: "frontend-next/dist/index.html", body: "i"}, entry{name: "frontend-next/dist/a.bin", body: chunk}, entry{name: "frontend-next/dist/b.bin", body: chunk}, entry{name: "frontend-next/dist/c.bin", body: chunk}), "reasonix-linux-amd64.tar.gz"); err == nil {
		t.Error("a page over the total bound was accepted")
	}
	big := []entry{{name: "frontend-next/dist/index.html", body: "i"}, {name: "frontend-next/dist/big.bin", body: strings.Repeat("a", maxPageFileBytes+1)}}
	if _, err := extractPage(tarGz(t, big...), "reasonix-linux-amd64.tar.gz"); err == nil {
		t.Error("an oversized file was accepted")
	}
}

func TestExtractBinaryStillFindsTheRootBinaryBesideAPage(t *testing.T) {
	got, err := extractBinary(tarGz(t, goodPage...), "reasonix-linux-amd64.tar.gz", "reasonix")
	if err != nil || string(got) != "new-binary" {
		t.Fatalf("got %q err %v", got, err)
	}
}

// An install the user made with a page beside the binary, plus things the
// upgrade has no business touching.
func pagedInstall(t *testing.T) (dir, exe string) {
	t.Helper()
	dir = testenv.TempDir(t)
	exe = filepath.Join(dir, "reasonix")
	must := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(exe, "old-binary")
	must(filepath.Join(dir, "frontend-next", "dist", "index.html"), "old-index")
	must(filepath.Join(dir, "frontend-next", "dist", "stale.js"), "stale")
	must(filepath.Join(dir, "frontend-next", "notes.txt"), "mine")
	must(filepath.Join(dir, "README"), "mine too")
	useExecutable(t, exe)
	return dir, exe
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func newTree(t *testing.T) *pageTree {
	t.Helper()
	tree, err := extractPage(tarGz(t, goodPage...), "reasonix-linux-amd64.tar.gz")
	if err != nil || tree == nil {
		t.Fatal(err)
	}
	return tree
}

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	var left []string
	entries, _ := os.ReadDir(filepath.Join(dir, "frontend-next"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".dist") {
			left = append(left, e.Name())
		}
	}
	return left
}

func TestUpgradeReplacesTheBinaryAndTheWholePageAndNothingElse(t *testing.T) {
	dir, exe := pagedInstall(t)
	if err := applyUpgrade([]byte("new-binary"), newTree(t)); err != nil {
		t.Fatal(err)
	}
	if got := read(t, exe); got != "new-binary" {
		t.Errorf("binary %q", got)
	}
	dist := filepath.Join(dir, "frontend-next", "dist")
	if read(t, filepath.Join(dist, "index.html")) != "new-index" || read(t, filepath.Join(dist, "assets", "app.js")) != "new-app" {
		t.Error("the page was not replaced")
	}
	if read(t, filepath.Join(dist, "stale.js")) != "<missing>" {
		t.Error("a file of the old page survived the replacement")
	}
	if read(t, filepath.Join(dir, "frontend-next", "notes.txt")) != "mine" || read(t, filepath.Join(dir, "README")) != "mine too" {
		t.Error("something outside frontend-next/dist was touched")
	}
	if left := leftovers(t, dir); len(left) != 0 {
		t.Errorf("leftovers %v", left)
	}
}

func TestUpgradeDoesNotAddAPageToAnInstallThatHasNone(t *testing.T) {
	dir := testenv.TempDir(t)
	exe := filepath.Join(dir, "reasonix")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	useExecutable(t, exe)
	if err := applyUpgrade([]byte("new-binary"), newTree(t)); err != nil {
		t.Fatal(err)
	}
	if read(t, exe) != "new-binary" {
		t.Error("binary not replaced")
	}
	if _, err := os.Stat(filepath.Join(dir, "frontend-next")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a page was added to an install that never had one: %v", err)
	}
}

func TestUpgradeLeavesAPagelessArchiveAlonePage(t *testing.T) {
	dir, exe := pagedInstall(t)
	if err := applyUpgrade([]byte("new-binary"), nil); err != nil {
		t.Fatal(err)
	}
	if read(t, exe) != "new-binary" || read(t, filepath.Join(dir, "frontend-next", "dist", "index.html")) != "old-index" {
		t.Error("an archive with no page must leave the existing page alone")
	}
}

// Old kernel and old page must stay together whichever step fails.
func TestUpgradeKeepsTheOldKernelAndOldPageWhenAStepFails(t *testing.T) {
	boom := errors.New("injected")
	for name, inject := range map[string]func(t *testing.T){
		"moving the old page aside": func(t *testing.T) {
			prev := upgradeRename
			upgradeRename = func(from, to string) error {
				if strings.HasSuffix(from, filepath.Join("frontend-next", "dist")) {
					return boom
				}
				return prev(from, to)
			}
			t.Cleanup(func() { upgradeRename = prev })
		},
		"moving the new page in": func(t *testing.T) {
			prev := upgradeRename
			upgradeRename = func(from, to string) error {
				if strings.Contains(filepath.Base(from), ".dist.new") {
					return boom
				}
				return prev(from, to)
			}
			t.Cleanup(func() { upgradeRename = prev })
		},
		"replacing the binary after the page is in": func(t *testing.T) {
			prev := applyBinary
			applyBinary = func([]byte) error { return boom }
			t.Cleanup(func() { applyBinary = prev })
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir, exe := pagedInstall(t)
			inject(t)
			if err := applyUpgrade([]byte("new-binary"), newTree(t)); err == nil {
				t.Fatal("the injected failure was swallowed")
			}
			dist := filepath.Join(dir, "frontend-next", "dist")
			if read(t, exe) != "old-binary" {
				t.Errorf("binary %q", read(t, exe))
			}
			if read(t, filepath.Join(dist, "index.html")) != "old-index" || read(t, filepath.Join(dist, "stale.js")) != "stale" {
				t.Error("the old page did not survive")
			}
			if read(t, filepath.Join(dist, "assets", "app.js")) != "<missing>" {
				t.Error("part of the new page is live")
			}
			if left := leftovers(t, dir); len(left) != 0 {
				t.Errorf("leftovers %v", left)
			}
		})
	}
}

// The page must stay as reachable as the one it replaces: a root-owned install
// served to an ordinary user needs the directories to be traversable.
func TestUpgradedPageKeepsTheModesOfTheOldOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports no Unix permission bits")
	}
	dir, _ := pagedInstall(t)
	if err := os.MkdirAll(filepath.Join(dir, "frontend-next", "dist", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyUpgrade([]byte("new-binary"), newTree(t)); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"", "assets"} {
		st, err := os.Stat(filepath.Join(dir, "frontend-next", "dist", rel))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o755 {
			t.Errorf("dist/%s is %v after the upgrade, want 0755", rel, st.Mode().Perm())
		}
	}
	st, err := os.Stat(filepath.Join(dir, "frontend-next", "dist", "index.html"))
	if err != nil || st.Mode().Perm() != 0o644 {
		t.Errorf("index.html is %v (%v), want 0644", st.Mode().Perm(), err)
	}
}

// A page directory the user pointed at their own build is theirs: the binary is
// upgraded and the link, with what it points at, is left exactly as it was.
func TestUpgradeLeavesALinkedPageAlone(t *testing.T) {
	for _, which := range []string{"dist", "frontend-next"} {
		t.Run(which, func(t *testing.T) {
			root := testenv.TempDir(t)
			install := filepath.Join(root, "install")
			mine := filepath.Join(root, "my-build")
			if err := os.MkdirAll(filepath.Join(mine, "dist"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(mine, "dist", "index.html"), []byte("mine"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(install, "frontend-next"), 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(install, "frontend-next", "dist")
			target := filepath.Join(mine, "dist")
			if which == "frontend-next" {
				if err := os.RemoveAll(filepath.Join(install, "frontend-next")); err != nil {
					t.Fatal(err)
				}
				link, target = filepath.Join(install, "frontend-next"), mine
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("no symlinks here: %v", err)
			}
			exe := filepath.Join(install, "reasonix")
			if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
				t.Fatal(err)
			}
			useExecutable(t, exe)

			if !pageDirIsLink() {
				t.Fatal("a linked page directory was not recognised")
			}
			if err := applyUpgrade([]byte("new-binary"), newTree(t)); err != nil {
				t.Fatal(err)
			}
			if read(t, exe) != "new-binary" {
				t.Error("the binary was not upgraded")
			}
			if got, err := os.Readlink(link); err != nil || got != target {
				t.Errorf("the link was replaced: %q %v", got, err)
			}
			if read(t, filepath.Join(mine, "dist", "index.html")) != "mine" {
				t.Error("what the link points at was changed")
			}
			if _, err := os.Stat(filepath.Join(install, "frontend-next", ".dist.old")); err == nil {
				t.Error("a page swap was attempted through the link")
			}
		})
	}
}

func TestAPlainPageDirectoryIsNotALink(t *testing.T) {
	pagedInstall(t)
	if pageDirIsLink() {
		t.Error("a real page directory was taken for a link")
	}
}
