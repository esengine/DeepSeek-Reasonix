package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/mod/semver"
)

// The acceptance rules of the updater shipped in v1.39.5, kept as they were
// there: installed binaries cannot change, so a pointer must satisfy these
// whatever the current code accepts.
var legacy139StableTag = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`)

func legacy139IsCLITag(tag string) bool {
	tag = strings.TrimSpace(tag)
	return len(tag) >= 2 && tag[0] == 'v' && tag[1] >= '0' && tag[1] <= '9'
}

func legacy139ExpectedAssetURL(raw, tag, name string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	want := fmt.Sprintf("/%s/%s/releases/download/%s/%s", ghOwner, ghRepo, tag, name)
	return strings.EqualFold(parsed.Hostname(), "github.com") &&
		parsed.Port() == "" &&
		parsed.EscapedPath() == want &&
		parsed.RawQuery == "" &&
		parsed.Fragment == ""
}

func legacy139Accepts(rel ghRelease) error {
	if !legacy139IsCLITag(rel.TagName) || !legacy139StableTag.MatchString(rel.TagName) || rel.Prerelease {
		return fmt.Errorf("tag %q prerelease=%v is not a stable CLI release", rel.TagName, rel.Prerelease)
	}
	for _, name := range requiredCLIAssets {
		found := false
		for _, asset := range rel.Assets {
			if asset.Name != name {
				continue
			}
			if found {
				return fmt.Errorf("duplicate asset %s", name)
			}
			found = true
			if asset.Size <= 0 || asset.Size > 1<<30 || !legacy139ExpectedAssetURL(asset.BrowserDownloadURL, rel.TagName, name) {
				return fmt.Errorf("asset %s: size %d url %s", name, asset.Size, asset.BrowserDownloadURL)
			}
		}
		if !found {
			return fmt.Errorf("missing asset %s", name)
		}
	}
	return nil
}

func loadStudioPointer(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "testdata", "cli-pointer-v2.24.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStudioPointerSatisfiesTheV1395UpdaterRules(t *testing.T) {
	var rel ghRelease
	if err := json.Unmarshal(loadStudioPointer(t), &rel); err != nil {
		t.Fatal(err)
	}
	if err := legacy139Accepts(rel); err != nil {
		t.Fatalf("v1.39.5 would refuse the pointer: %v", err)
	}
	if semver.Compare(rel.TagName, "v1.39.5") <= 0 {
		t.Fatalf("%s does not sort above the 1.x line", rel.TagName)
	}
}

func TestStudioTagShapesTheV1395UpdaterRefuses(t *testing.T) {
	var rel ghRelease
	if err := json.Unmarshal(loadStudioPointer(t), &rel); err != nil {
		t.Fatal(err)
	}
	rewrite := func(tag string) ghRelease {
		out := rel
		out.TagName = tag
		out.Assets = append([]ghAsset(nil), rel.Assets...)
		for i := range out.Assets {
			out.Assets[i].BrowserDownloadURL = strings.ReplaceAll(rel.Assets[i].BrowserDownloadURL, "/"+rel.TagName+"/", "/"+tag+"/")
		}
		return out
	}
	for _, tag := range []string{"studio-v2.24.0", "v2.24.0-rc.1", "v2.24.0-preview.1"} {
		if legacy139Accepts(rewrite(tag)) == nil {
			t.Errorf("v1.39.5 accepts %q as the stable pointer", tag)
		}
	}
	mismatched := rel
	mismatched.Assets = append([]ghAsset(nil), rel.Assets...)
	mismatched.Assets[0].BrowserDownloadURL = strings.Replace(rel.Assets[0].BrowserDownloadURL, "/v2.24.0/", "/studio-v2.24.0/", 1)
	if legacy139Accepts(mismatched) == nil {
		t.Error("v1.39.5 accepts an asset URL under a tag other than the pointer's")
	}
}

func TestCurrentUpdaterReadsTheStudioPointer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(loadStudioPointer(t))
	}))
	defer server.Close()
	rel, err := fetchCLIReleasePointer(server.Client(), server.URL, cliReleaseStable)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range [][2]string{
		{"darwin", "amd64"}, {"darwin", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"},
	} {
		if findCLIPlatformAsset(rel, target[0], target[1]) == nil {
			t.Errorf("no asset for %s/%s", target[0], target[1])
		}
	}
	if findCLIReleaseAsset(rel, "SHA256SUMS") == nil {
		t.Error("no SHA256SUMS")
	}
}
