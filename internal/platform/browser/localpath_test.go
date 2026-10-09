package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type localPathCase struct {
	In   string `json:"in"`
	Out  string `json:"out"`
	Kind string `json:"kind"`
}

func TestLocalPathURLTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "local_paths.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []localPathCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		got, kind := LocalPathURL(tc.In)
		if got != tc.Out || string(kind) != tc.Kind {
			t.Errorf("LocalPathURL(%q) = %q, %q; want %q, %q", tc.In, got, kind, tc.Out, tc.Kind)
		}
	}
}

func TestCheckURLReadsATypedLocalPath(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "页面 1.html")
	if err := os.WriteFile(page, []byte("<p>x</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "o.html")
	if err := os.WriteFile(outside, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := checkURL(page, []string{root}); err != nil {
		t.Errorf("a path inside the workspace was refused: %v", err)
	}
	if got, err := checkURL(outside, []string{root}); CodeOf(err) != CodeURLRefused {
		t.Errorf("a path outside the workspace = %q, %v; want %s", got, err, CodeURLRefused)
	}
	if _, err := checkURL(`\\nas\share\x.html`, []string{root}); CodeOf(err) != CodeNetworkPath {
		t.Errorf("a network path was not refused: %v", err)
	}
}

func TestOriginOfReadsAPathAsItsFileURL(t *testing.T) {
	for in, want := range map[string]string{
		"D:/x/y.html":           "file://",
		"file:///D:/x/y.html":   "file://",
		"/tmp/x.html":           "file://",
		`\\nas\share\x.html`:    "",
		"file:////host/x":       "",
		"https://Example.com/a": "https://example.com",
		"example.com":           "",
	} {
		if got := OriginOf(in); got != want {
			t.Errorf("OriginOf(%q) = %q, want %q", in, got, want)
		}
	}
	s := &Session{cfg: Config{Roots: []string{t.TempDir()}}}
	if s.ServesWorkspace(`\\nas\share\x.html`) || s.ServesWorkspace("file:////host/x") {
		t.Error("a share counted as the workspace's own page")
	}
}
