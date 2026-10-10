package serve

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestTitleRoutesRejectUnownedPaths(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	prov := &recordingTitleProvider{}
	rt.Server.titleProv = prov
	dir := filepath.Dir(path)
	before, err := os.ReadFile(path + ".meta")
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(dir, ".session-titles.json")
	cacheBefore, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	type pathCase struct {
		name, path string
		status     int
	}
	cases := []pathCase{
		{"outside", filepath.Join(testenv.TempDir(t), filepath.Base(path)), http.StatusForbidden},
		{"sibling-prefix", filepath.Join(dir+"-outside", filepath.Base(path)), http.StatusForbidden},
		{"nested", filepath.Join(dir, "nested", filepath.Base(path)), http.StatusForbidden},
		{"parent-traversal", dir + string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Base(path), http.StatusForbidden},
		{"sidecar", path + ".meta", http.StatusBadRequest},
		{"missing", filepath.Join(dir, "missing.jsonl"), http.StatusNotFound},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			pathCase{"alternate-stream", filepath.Join(dir, "session.jsonl:stream.jsonl"), http.StatusBadRequest},
			pathCase{"reserved-device", filepath.Join(dir, "NUL"), http.StatusBadRequest},
		)
	}
	for _, route := range []string{"rename", "auto-name"} {
		for _, tc := range cases {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				res := titlePost(t, h, route, tc.path, "must not save")
				if res.Code != tc.status {
					t.Fatalf("status=%d want=%d: %s", res.Code, tc.status, res.Body.String())
				}
			})
		}
	}
	after, err := os.ReadFile(path + ".meta")
	if err != nil {
		t.Fatal(err)
	}
	cacheAfter, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !bytes.Equal(cacheBefore, cacheAfter) || prov.count() != 0 {
		t.Fatal("rejected paths changed a title or called the model")
	}
}

func TestTitleRoutesRejectEscapingTranscriptSymlink(t *testing.T) {
	h, rt, path, _ := titleSession(t)
	prov := &recordingTitleProvider{}
	rt.Server.titleProv = prov
	outside := filepath.Join(testenv.TempDir(t), "external.jsonl")
	original := []byte("external file must remain unchanged\n")
	if err := os.WriteFile(outside, original, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "escape.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, route := range []string{"rename", "auto-name"} {
		res := titlePost(t, h, route, link, "must not save")
		if res.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d: %s", route, res.Code, res.Body.String())
		}
	}
	after, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) || prov.count() != 0 {
		t.Fatal("escaping symlink accessed the model or changed the outside file")
	}
	if _, err := os.Stat(link + ".meta"); !os.IsNotExist(err) {
		t.Fatalf("unexpected metadata for rejected symlink: %v", err)
	}
}
