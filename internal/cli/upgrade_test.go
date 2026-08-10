package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"dev", "", false},
		{"", "", false},
		{"  ", "", false},
		{"abc", "", false},
		{"v1.2.3", "v1.2.3", true},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3-rc1", "v1.2.3-rc1", true},
		{"  v0.10.0  ", "v0.10.0", true},
	}
	for _, tt := range tests {
		got, ok := normalizeVersion(tt.in)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("normalizeVersion(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	content := []byte("hello world")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])

	t.Run("match", func(t *testing.T) {
		checksumFile := fmt.Appendf(nil, "%s  reasonix-linux-amd64.tar.gz\n", hash)
		if err := verifyChecksum(content, "reasonix-linux-amd64.tar.gz", checksumFile); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		checksumFile := fmt.Appendf(nil, "%s  reasonix-linux-amd64.tar.gz\n", "0000000000000000000000000000000000000000000000000000000000000000")
		if err := verifyChecksum(content, "reasonix-linux-amd64.tar.gz", checksumFile); err == nil {
			t.Error("expected checksum mismatch error")
		}
	})

	t.Run("not found", func(t *testing.T) {
		checksumFile := fmt.Appendf(nil, "%s  reasonix-darwin-arm64.tar.gz\n", hash)
		if err := verifyChecksum(content, "reasonix-linux-amd64.tar.gz", checksumFile); err == nil {
			t.Error("expected not-found error")
		}
	})
}

func TestUpgradeSuccessMessageIncludesCurrentAndLatestVersions(t *testing.T) {
	cur := "v1.10.0"
	latest := "v1.11.0"

	got := upgradeSuccessMessage(cur, latest)
	if !strings.Contains(got, cur) {
		t.Fatalf("success message %q does not include current version %q", got, cur)
	}
	if !strings.Contains(got, latest) {
		t.Fatalf("success message %q does not include latest version %q", got, latest)
	}
	if strings.Index(got, cur) > strings.Index(got, latest) {
		t.Fatalf("success message %q should report current version before latest version", got)
	}
	if strings.Contains(got, "%!") {
		t.Fatalf("success message %q contains a missing fmt argument marker", got)
	}
}

func TestExtractFromTarGz(t *testing.T) {
	// Build a .tar.gz in memory containing a "reasonix" entry.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte("fake binary content")
	if err := tw.WriteHeader(&tar.Header{
		Name: "reasonix",
		Mode: 0o755,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractFromTarGz(buf.Bytes(), "reasonix")
	if err != nil {
		t.Fatalf("extractFromTarGz: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("extracted body = %q, want %q", got, body)
	}
}

func TestExtractFromTarGz_Nested(t *testing.T) {
	// Archives from goreleaser have the binary at the root with its name.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte("nested binary")
	if err := tw.WriteHeader(&tar.Header{
		Name: "reasonix-linux-amd64/reasonix",
		Mode: 0o755,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractFromTarGz(buf.Bytes(), "reasonix")
	if err != nil {
		t.Fatalf("extractFromTarGz: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("extracted body = %q, want %q", got, body)
	}
}

func TestExtractFromTarGz_NotFound(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{
		Name: "other-file.txt",
		Mode: 0o644,
		Size: 3,
	}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte("foo"))
	tw.Close()
	gw.Close()

	_, err := extractFromTarGz(buf.Bytes(), "reasonix")
	if err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestIsCLITag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"v1.6.0", true},
		{"v0.1.0", true},
		{"v2.0.0-rc.1", true},
		{"desktop-v1.5.0", false},
		{"npm-v1.4.0", false},
		{"", false},
		{"v", false},
	}
	for _, tt := range tests {
		if got := isCLITag(tt.tag); got != tt.want {
			t.Errorf("isCLITag(%q) = %v, want %v", tt.tag, got, tt.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{500, "500 B"},
		{2048, "2.0 KiB"},
		{19_000_000, "18.1 MiB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.bytes); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func completeCLIRelease(tag string, prerelease bool) ghRelease {
	assets := make([]ghAsset, 0, len(requiredCLIAssets))
	for _, name := range requiredCLIAssets {
		assets = append(assets, ghAsset{
			Name:               name,
			BrowserDownloadURL: fmt.Sprintf("https://github.com/esengine/DeepSeek-Reasonix/releases/download/%s/%s", tag, name),
			Size:               42,
		})
	}
	return ghRelease{TagName: tag, Prerelease: prerelease, Assets: assets}
}

func TestPickCLIRelease(t *testing.T) {
	pick := func(rels []ghRelease, channel cliReleaseChannel) string {
		if r := pickCLIRelease(rels, channel); r != nil {
			return r.TagName
		}
		return ""
	}

	// Stable skips foreign namespaces and every prerelease, even when a Preview
	// was published more recently than the latest Stable release.
	mixed := []ghRelease{
		completeCLIRelease("v1.18.0-preview.1", true),
		{TagName: "desktop-v1.18.0"},
		{TagName: "npm-v1.18.0"},
		completeCLIRelease("v1.6.0", false),
	}
	if got := pick(mixed, cliReleaseStable); got != "v1.6.0" {
		t.Errorf("stable channel: got %q, want v1.6.0", got)
	}

	prereleases := []ghRelease{
		completeCLIRelease("v1.18.0-preview.2", true),
		completeCLIRelease("v1.19.0-rc.1", true),
		completeCLIRelease("v1.18.0-preview.12", true),
	}
	if got := pick(prereleases, cliReleaseStable); got != "" {
		t.Errorf("official release selection accepted prerelease %q", got)
	}

	incomplete := completeCLIRelease("v1.7.0", false)
	incomplete.Assets = incomplete.Assets[:len(incomplete.Assets)-1]
	if got := pick([]ghRelease{incomplete, completeCLIRelease("v1.6.0", false)}, cliReleaseStable); got != "v1.6.0" {
		t.Errorf("incomplete newest Stable release: got %q, want v1.6.0", got)
	}

	insecure := completeCLIRelease("v1.7.0", false)
	insecure.Assets[0].BrowserDownloadURL = "http://example.invalid/reasonix.tar.gz"
	if got := pick([]ghRelease{insecure, completeCLIRelease("v1.6.0", false)}, cliReleaseStable); got != "v1.6.0" {
		t.Errorf("release with insecure asset URL: got %q, want v1.6.0", got)
	}

	spoofed := completeCLIRelease("v1.7.0", false)
	spoofed.Assets[0].BrowserDownloadURL = "https://github.com@evil.invalid/esengine/DeepSeek-Reasonix/releases/download/v1.7.0/reasonix-darwin-amd64.tar.gz"
	if got := pick([]ghRelease{spoofed, completeCLIRelease("v1.6.0", false)}, cliReleaseStable); got != "v1.6.0" {
		t.Errorf("release with spoofed asset host: got %q, want v1.6.0", got)
	}

	wrongTag := completeCLIRelease("v1.7.0", false)
	wrongTag.Assets[0].BrowserDownloadURL = "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.6.0/reasonix-darwin-amd64.tar.gz"
	if got := pick([]ghRelease{wrongTag, completeCLIRelease("v1.6.0", false)}, cliReleaseStable); got != "v1.6.0" {
		t.Errorf("release with cross-tag asset URL: got %q, want v1.6.0", got)
	}

	empty := completeCLIRelease("v1.7.0", false)
	empty.Assets[0].Size = 0
	if got := pick([]ghRelease{empty, completeCLIRelease("v1.6.0", false)}, cliReleaseStable); got != "v1.6.0" {
		t.Errorf("release with zero-byte asset: got %q, want v1.6.0", got)
	}

	duplicate := completeCLIRelease("v1.7.0", false)
	duplicate.Assets = append(duplicate.Assets, duplicate.Assets[0])
	if got := pick([]ghRelease{duplicate, completeCLIRelease("v1.6.0", false)}, cliReleaseStable); got != "v1.6.0" {
		t.Errorf("release with duplicate required asset: got %q, want v1.6.0", got)
	}

	if got := pick([]ghRelease{{TagName: "desktop-v1.0.0"}}, cliReleaseStable); got != "" {
		t.Errorf("no CLI release should return nil, got %q", got)
	}
}

func TestFindCLIPlatformAssetRequiresExactArchiveName(t *testing.T) {
	release := completeCLIRelease("v1.18.0", false)
	release.Assets = append([]ghAsset{{
		Name:               "reasonix-linux-amd64.signature",
		BrowserDownloadURL: "https://example.invalid/signature",
	}}, release.Assets...)

	asset := findCLIPlatformAsset(&release, "linux", "amd64")
	if asset == nil || asset.Name != "reasonix-linux-amd64.tar.gz" {
		t.Fatalf("Linux asset = %+v, want exact tar.gz archive", asset)
	}
	if got := cliPlatformAssetName("windows", "arm64"); got != "reasonix-windows-arm64.zip" {
		t.Fatalf("Windows asset name = %q, want reasonix-windows-arm64.zip", got)
	}

	expectedURL := asset.BrowserDownloadURL
	release.Assets = append([]ghAsset{{
		Name:               "reasonix-linux-amd64.tar.gz",
		BrowserDownloadURL: "https://evil.invalid/reasonix-linux-amd64.tar.gz",
	}}, release.Assets...)
	asset = findCLIPlatformAsset(&release, "linux", "amd64")
	if asset == nil || asset.BrowserDownloadURL != expectedURL {
		t.Fatalf("platform selection accepted an unsafe duplicate: %+v", asset)
	}

	for i := range release.Assets {
		if release.Assets[i].Name == "reasonix-linux-amd64.tar.gz" &&
			release.Assets[i].BrowserDownloadURL == expectedURL {
			release.Assets[i].Size = 0
		}
	}
	if asset := findCLIPlatformAsset(&release, "linux", "amd64"); asset != nil {
		t.Fatalf("platform selection accepted a zero-byte archive: %+v", asset)
	}
}

func TestValidateCLIUpgradeRedirect(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		wantError bool
	}{
		{name: "GitHub HTTPS asset redirect", target: "https://release-assets.githubusercontent.com/file"},
		{name: "GitHub redirect", target: "https://github.com/file"},
		{name: "HTTPS downgrade", target: "http://release-assets.githubusercontent.com/file", wantError: true},
		{name: "userinfo", target: "https://user@release-assets.githubusercontent.com/file", wantError: true},
		{name: "missing hostname", target: "https:///file", wantError: true},
		{name: "untrusted HTTPS host", target: "https://example.invalid/file", wantError: true},
		{name: "githubusercontent suffix spoof", target: "https://release-assets.githubusercontent.com.evil.invalid/file", wantError: true},
		{name: "explicit port", target: "https://release-assets.githubusercontent.com:443/file", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tt.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = validateCLIUpgradeRedirect(req, nil)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateCLIUpgradeRedirect(%q) error = %v, wantError=%v", tt.target, err, tt.wantError)
			}
		})
	}
	t.Run("redirect limit", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/file", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateCLIUpgradeRedirect(req, make([]*http.Request, 10)); err == nil {
			t.Fatal("validateCLIUpgradeRedirect accepted more than 10 redirects")
		}
	})
}

func TestCLIReleaseChannelContract(t *testing.T) {
	if !strings.HasSuffix(ghAPIReleases, "?per_page=100") {
		t.Fatalf("CLI release query must retain enough history to skip archived prereleases: %q", ghAPIReleases)
	}

	for _, tc := range []struct {
		value string
		want  cliReleaseChannel
		ok    bool
	}{
		{"", cliReleaseStable, true},
		{"stable", cliReleaseStable, true},
		{"PREVIEW", cliReleaseStable, true},
		{"canary", cliReleaseStable, true},
		{"next", cliReleaseStable, true},
		{"rc", "", false},
	} {
		got, err := parseCLIReleaseChannel(tc.value)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("parseCLIReleaseChannel(%q) = (%q, %v), want (%q, ok=%v)", tc.value, got, err, tc.want, tc.ok)
		}
	}

	for _, tc := range []struct {
		version string
		channel cliReleaseChannel
		want    bool
	}{
		{"v1.17.21", cliReleaseStable, true},
		{"v1.18.0-preview.1", cliReleaseStable, false},
		{"v1.18.0-rc.1", cliReleaseStable, false},
	} {
		if got := versionBelongsToCLIChannel(tc.version, tc.channel); got != tc.want {
			t.Errorf("versionBelongsToCLIChannel(%q, %q) = %v, want %v", tc.version, tc.channel, got, tc.want)
		}
	}
}

func TestParseAndResolveCLIUpgradeChannel(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		configured string
		want       cliReleaseChannel
		wantSave   bool
		wantCheck  bool
		wantForce  bool
	}{
		{name: "fresh default", configured: "", want: cliReleaseStable},
		{name: "saved preview migrates", configured: "preview", want: cliReleaseStable},
		{name: "legacy preview positional", args: []string{"preview"}, configured: "stable", want: cliReleaseStable},
		{name: "stable positional", args: []string{"--check", "stable"}, configured: "preview", want: cliReleaseStable, wantCheck: true},
		{name: "legacy flags after positional", args: []string{"preview", "--force"}, configured: "stable", want: cliReleaseStable, wantForce: true},
		{name: "legacy one off override", args: []string{"--channel", "preview"}, configured: "stable", want: cliReleaseStable},
		{name: "mixed legacy aliases", args: []string{"preview", "--channel=stable"}, configured: "stable", want: cliReleaseStable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			syntax, err := parseCLIUpgradeSyntax(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			got, save, err := resolveCLIUpgradeChannel(syntax, tc.configured)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || save != tc.wantSave || syntax.checkOnly != tc.wantCheck || syntax.force != tc.wantForce {
				t.Fatalf("resolved = (%q, save=%v, check=%v, force=%v), want (%q, save=%v, check=%v, force=%v)",
					got, save, syntax.checkOnly, syntax.force, tc.want, tc.wantSave, tc.wantCheck, tc.wantForce)
			}
		})
	}
}

func TestParseCLIUpgradeChannelRejectsAmbiguousArguments(t *testing.T) {
	for _, args := range [][]string{
		{"stable", "preview"},
		{"--channel", "rc"},
		{"--channel"},
		{"--channel="},
	} {
		if _, err := parseCLIUpgradeSyntax(args); err == nil {
			t.Errorf("parseCLIUpgradeSyntax(%q) unexpectedly succeeded", args)
		}
	}
}

func TestUpgradeCommandRejectsMalformedConfigWithoutPanicking(t *testing.T) {
	oldLoad := loadCLIUpgradeConfig
	loadCLIUpgradeConfig = func() (*config.Config, error) {
		return nil, errors.New("malformed TOML")
	}
	t.Cleanup(func() { loadCLIUpgradeConfig = oldLoad })

	if code := upgradeCommand([]string{"--channel", "stable"}, "v1.17.0"); code != 1 {
		t.Fatalf("upgradeCommand exit = %d, want 1", code)
	}
}

func TestPersistCLIReleaseChannelRemovesLegacyConfig(t *testing.T) {
	for _, legacy := range []string{"stable", "preview", "canary", "beta", "next"} {
		t.Run(legacy, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("REASONIX_HOME", home)
			if err := os.WriteFile(config.UserConfigPath(), []byte("[cli]\nupdate_channel = \""+legacy+"\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := persistCLIReleaseChannel(cliReleaseStable); err != nil {
				t.Fatalf("migrate legacy channel: %v", err)
			}
			cfg, err := config.LoadForEditReadOnlyStrict(config.UserConfigPath())
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.CLIUpdateChannel(); got != "stable" {
				t.Fatalf("saved CLI channel = %q, want stable", got)
			}
			raw, err := os.ReadFile(config.UserConfigPath())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "[cli]") || strings.Contains(string(raw), "update_channel") {
				t.Fatalf("saved config retained retired CLI channel:\n%s", raw)
			}
		})
	}
}

func TestFetchCLIReleasePointer(t *testing.T) {
	valid := completeCLIRelease("v1.18.0", false)
	invalidMetadata := completeCLIRelease("v1.18.0-preview.1", true)
	incomplete := completeCLIRelease("v1.18.0", false)
	incomplete.Assets = incomplete.Assets[:len(incomplete.Assets)-1]
	insecure := completeCLIRelease("v1.18.0", false)
	insecure.Assets[0].BrowserDownloadURL = "http://example.invalid/reasonix.tar.gz"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != "reasonix-cli" {
			t.Errorf("unexpected pointer request headers: Accept=%q User-Agent=%q", r.Header.Get("Accept"), r.Header.Get("User-Agent"))
		}
		var release ghRelease
		switch r.URL.Path {
		case "/valid":
			release = valid
		case "/incomplete":
			release = incomplete
		case "/insecure":
			release = insecure
		default:
			release = invalidMetadata
		}
		if err := json.NewEncoder(w).Encode(release); err != nil {
			t.Errorf("encode release pointer: %v", err)
		}
	}))
	defer server.Close()

	release, err := fetchCLIReleasePointer(server.Client(), server.URL+"/valid", cliReleaseStable)
	if err != nil || release.TagName != "v1.18.0" {
		t.Fatalf("valid official pointer = (%+v, %v)", release, err)
	}
	if _, err := fetchCLIReleasePointer(server.Client(), server.URL+"/invalid", cliReleaseStable); err == nil {
		t.Fatal("prerelease pointer should fail closed")
	}
	if _, err := fetchCLIReleasePointer(server.Client(), server.URL+"/incomplete", cliReleaseStable); err == nil {
		t.Fatal("pointer missing a required CLI asset should fall back")
	}
	if _, err := fetchCLIReleasePointer(server.Client(), server.URL+"/insecure", cliReleaseStable); err == nil {
		t.Fatal("pointer with an insecure asset URL should fall back")
	}
}

type upgradeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn upgradeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestFetchLatestReleaseFallsThroughIncompletePointerAndGitHubRelease(t *testing.T) {
	incompletePointer := completeCLIRelease("v1.8.0", false)
	incompletePointer.Assets = incompletePointer.Assets[:len(incompletePointer.Assets)-1]
	incompleteGitHub := completeCLIRelease("v1.7.0", false)
	incompleteGitHub.Assets = incompleteGitHub.Assets[:len(incompleteGitHub.Assets)-1]
	completeGitHub := completeCLIRelease("v1.6.0", false)

	client := &http.Client{Transport: upgradeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload any
		switch request.URL.String() {
		case cliGatewayBase + "/stable/latest.json":
			payload = incompletePointer
		case ghAPIReleases:
			payload = []ghRelease{incompleteGitHub, completeGitHub}
		default:
			t.Fatalf("unexpected release request: %s", request.URL)
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    request,
		}, nil
	})}

	release, err := fetchLatestRelease(client, cliReleaseStable)
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if release.TagName != "v1.6.0" {
		t.Fatalf("fallback release = %q, want v1.6.0", release.TagName)
	}
}

func TestFetchBytesSizedRequiresExactReleaseAssetLength(t *testing.T) {
	client := &http.Client{Transport: upgradeRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(request.URL.Query().Get("body"))),
			Request:    request,
		}, nil
	})}

	if data, err := fetchBytesSized(client, "https://example.invalid/archive?body=exact", 5); err != nil || string(data) != "exact" {
		t.Fatalf("exact release asset = %q, %v", data, err)
	}
	if _, err := fetchBytesSized(client, "https://example.invalid/archive?body=short", 6); err == nil {
		t.Fatal("fetchBytesSized accepted fewer bytes than the release declared")
	}
	if _, err := fetchBytesSized(client, "https://example.invalid/archive?body=longer", 5); err == nil {
		t.Fatal("fetchBytesSized accepted more bytes than the release declared")
	}
	if _, err := fetchBytesSized(client, "https://example.invalid/archive?body=", 0); err == nil {
		t.Fatal("fetchBytesSized accepted a zero expected size")
	}
	if _, err := fetchBytesSized(client, "https://example.invalid/archive?body=x", maxCLIReleaseAssetSize+1); err == nil {
		t.Fatal("fetchBytesSized accepted a size above the release maximum")
	}
}
