package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/desktop/internal/update"
	"reasonix/internal/installlayout"
	"reasonix/internal/repair"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"dev", "", false},
		{"", "", false},
		{"  ", "", false},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3", "v1.2.3", true},
		{"v1.2", "v1.2.0", true}, // semver.Canonical fills the patch
		{"garbage", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeVersion(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("normalizeVersion(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestValidateUpdaterRequestBindsChannelVersionAndID(t *testing.T) {
	tests := []struct {
		name    string
		request string
		channel string
		version string
		wantErr bool
	}{
		{name: "stable", request: "web-stable-1", channel: "stable", version: "v1.18.0"},
		{name: "legacy preview selects official", request: "web-preview-1", channel: "preview", version: "v1.18.0"},
		{name: "legacy preview rejects prerelease", request: "web-preview-2", channel: "preview", version: "v1.18.0-preview.1", wantErr: true},
		{name: "stable rejects preview version", request: "web-stable-2", channel: "stable", version: "v1.18.0-preview.1", wantErr: true},
		{name: "empty request", channel: "stable", version: "v1.18.0", wantErr: true},
		{name: "unsafe request", request: "web request", channel: "stable", version: "v1.18.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, selected, version, err := validateUpdaterRequest(tt.request, tt.channel, tt.version)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateUpdaterRequest() error = %v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if request != tt.request || selected != "stable" || version != tt.version {
				t.Fatalf("validateUpdaterRequest() = (%q, %q, %q)", request, selected, version)
			}
		})
	}
}

func TestValidateAssetInstallLayout(t *testing.T) {
	if err := validateAssetInstallLayout(""); err != nil {
		t.Fatalf("empty layout must remain accepted for legacy assets: %v", err)
	}
	if err := validateAssetInstallLayout("versioned-v1"); err != nil {
		t.Fatalf("versioned-v1 must be accepted: %v", err)
	}
	if err := validateAssetInstallLayout("unknown-layout"); err == nil {
		t.Fatal("unknown install_layout must be rejected")
	}
}

func TestUpdaterWailsMethodContracts(t *testing.T) {
	appType := reflect.TypeFor[*App]()
	tests := []struct {
		name   string
		numIn  int
		numOut int
	}{
		{name: "ApplyUpdateRequest", numIn: 4, numOut: 1},
		{name: "CheckUpdate", numIn: 2, numOut: 2},
		{name: "OpenDownloadPage", numIn: 1, numOut: 0},
	}
	// Legacy Download/Install split bindings must stay deleted (v1.20+).
	for _, removed := range []string{"DownloadUpdate", "InstallUpdate", "DownloadUpdateRequest", "InstallUpdateRequest", "ApplyUpdate"} {
		if _, ok := appType.MethodByName(removed); ok {
			t.Fatalf("App.%s must be removed from the Wails surface", removed)
		}
	}
	for _, tt := range tests {
		method, ok := appType.MethodByName(tt.name)
		if !ok {
			t.Fatalf("App.%s is missing", tt.name)
		}
		if method.Type.NumIn() != tt.numIn || method.Type.NumOut() != tt.numOut {
			t.Fatalf(
				"App.%s signature = %v inputs/%v outputs, want %v/%v",
				tt.name,
				method.Type.NumIn(),
				method.Type.NumOut(),
				tt.numIn,
				tt.numOut,
			)
		}
	}
}

func TestUpdaterNativeOperationsFailFastWhileBusy(t *testing.T) {
	app := NewApp()
	finishFirst, err := app.beginUpdaterOperation("first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.beginUpdaterOperation("second"); !errors.Is(err, errUpdateInProgress) {
		t.Fatalf("second updater operation error = %v, want errUpdateInProgress", err)
	}
	finishFirst()
	finishSecond, err := app.beginUpdaterOperation("second")
	if err != nil {
		t.Fatalf("operation did not become available after release: %v", err)
	}
	finishSecond()
}

func TestUpdaterReconcilesPendingUpdateBeforeInstallModeDispatch(t *testing.T) {
	originalExists := pendingUpdateExistsForInstall
	originalArchive := archiveSupersededPendingUpdateForInstall
	originalReconcile := reconcilePendingUpdateForInstall
	t.Cleanup(func() {
		pendingUpdateExistsForInstall = originalExists
		archiveSupersededPendingUpdateForInstall = originalArchive
		reconcilePendingUpdateForInstall = originalReconcile
	})

	called := false
	pendingUpdateExistsForInstall = func() bool { return true }
	archiveSupersededPendingUpdateForInstall = func() (bool, error) { return false, nil }
	reconcilePendingUpdateForInstall = func(runningVersion string) (repair.PendingUpdateReconcileResult, error) {
		called = true
		if runningVersion != version {
			t.Fatalf("running version = %q, want %q", runningVersion, version)
		}
		return repair.PendingUpdateReconcileResult{Pending: true, Cleared: true}, nil
	}
	meta := &cachedUpdate{Channel: "preview", Version: "v1.18.0-preview.65", Size: 42}
	if err := (&App{}).reconcilePendingUpdateForRequest("install-1", meta); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("pending update reconciliation was skipped")
	}
}

func TestUpdaterArchivesSupersededUpdateBeforeReconciliation(t *testing.T) {
	originalExists := pendingUpdateExistsForInstall
	originalArchive := archiveSupersededPendingUpdateForInstall
	originalReconcile := reconcilePendingUpdateForInstall
	t.Cleanup(func() {
		pendingUpdateExistsForInstall = originalExists
		archiveSupersededPendingUpdateForInstall = originalArchive
		reconcilePendingUpdateForInstall = originalReconcile
	})

	archived := false
	pendingUpdateExistsForInstall = func() bool { return true }
	archiveSupersededPendingUpdateForInstall = func() (bool, error) {
		archived = true
		return true, nil
	}
	reconcilePendingUpdateForInstall = func(string) (repair.PendingUpdateReconcileResult, error) {
		if !archived {
			t.Fatal("reconciliation ran before superseded update archival")
		}
		return repair.PendingUpdateReconcileResult{}, nil
	}
	meta := &cachedUpdate{Channel: "stable", Version: "v1.20.0", Size: 42}
	if err := (&App{}).reconcilePendingUpdateForRequest("install-1", meta); err != nil {
		t.Fatal(err)
	}
}

func TestUpdaterReconcilesBeforeDownloading(t *testing.T) {
	originalExists := pendingUpdateExistsForInstall
	originalArchive := archiveSupersededPendingUpdateForInstall
	originalReconcile := reconcilePendingUpdateForInstall
	t.Cleanup(func() {
		pendingUpdateExistsForInstall = originalExists
		archiveSupersededPendingUpdateForInstall = originalArchive
		reconcilePendingUpdateForInstall = originalReconcile
	})

	pendingUpdateExistsForInstall = func() bool { return true }
	archiveSupersededPendingUpdateForInstall = func() (bool, error) { return false, nil }
	reconcilePendingUpdateForInstall = func(string) (repair.PendingUpdateReconcileResult, error) {
		return repair.PendingUpdateReconcileResult{Pending: true}, errors.New("blocked before download")
	}
	err := (&App{}).ApplyUpdateRequest("stable", "v1.20.0", "preflight-recovery")
	if err == nil || !strings.Contains(err.Error(), "blocked before download") {
		t.Fatalf("pre-download recovery error=%v", err)
	}
}

func TestUpdaterBlocksInstallWhilePreviousReleaseAwaitsHealth(t *testing.T) {
	originalExists := pendingUpdateExistsForInstall
	originalArchive := archiveSupersededPendingUpdateForInstall
	originalReconcile := reconcilePendingUpdateForInstall
	t.Cleanup(func() {
		pendingUpdateExistsForInstall = originalExists
		archiveSupersededPendingUpdateForInstall = originalArchive
		reconcilePendingUpdateForInstall = originalReconcile
	})

	pendingUpdateExistsForInstall = func() bool { return true }
	archiveSupersededPendingUpdateForInstall = func() (bool, error) {
		return false, errors.New("not a superseded flat-layout transaction")
	}
	reconcilePendingUpdateForInstall = func(string) (repair.PendingUpdateReconcileResult, error) {
		return repair.PendingUpdateReconcileResult{Pending: true, AwaitingHealth: true}, repair.ErrPendingUpdateAwaitingHealth
	}
	meta := &cachedUpdate{Channel: "preview", Version: "v1.18.0-preview.65", Size: 42}
	err := (&App{}).reconcilePendingUpdateForRequest("install-1", meta)
	if err == nil || !strings.Contains(err.Error(), "startup health check") {
		t.Fatalf("health-check recovery error = %v", err)
	}
}

func TestExpectedUpdateVersionRejectsAdvancedPointer(t *testing.T) {
	if err := ensureExpectedUpdateVersion("preview", "v1.18.0-preview.1", "v1.18.0-preview.2"); err == nil {
		t.Fatal("advanced pointer unexpectedly matched the checked version")
	}
	if err := ensureExpectedUpdateVersion("stable", "v1.18.0", "v1.18.0"); err != nil {
		t.Fatalf("identical pointer rejected: %v", err)
	}
}

func TestUpdateSiblingNamesCoverEveryReplacedEntryPoint(t *testing.T) {
	windows := strings.Join(updateSiblingNames("windows"), "\x00")
	for _, want := range []string{"reasonix-guard.exe", "reasonix-launcher.exe", "reasonix-update-helper.exe", "reasonix-cli.exe", "Reasonix.exe"} {
		if !strings.Contains(windows, want) {
			t.Errorf("Windows release unit omits %q: %q", want, windows)
		}
	}
	if strings.Contains(windows, "reasonix.exe") {
		t.Fatalf("Windows release unit reintroduces the case-only CLI/launcher collision: %q", windows)
	}
	if got := updateSiblingNames("linux"); len(got) != 2 || got[0] != "reasonix-guard" || got[1] != "reasonix" {
		t.Fatalf("Linux release unit = %q", got)
	}
	if got := updateSiblingNames("darwin"); got != nil {
		t.Fatalf("macOS app-bundle update must not list file siblings: %q", got)
	}
}

func TestEvaluate(t *testing.T) {
	mk := func(version string) *update.Manifest {
		return &update.Manifest{
			Version:   version,
			Notes:     "notes",
			Platforms: map[string]update.Asset{update.CurrentPlatform(): {Size: 999}},
		}
	}
	portable := installProfile{
		Mode:          installModePortable,
		CanSelfUpdate: runtime.GOOS != "darwin",
		ArtifactKind:  artifactKindTarball,
	}
	if runtime.GOOS == "darwin" {
		portable.Mode = installModeManual
		portable.ManualReason = manualUpdateReason()
	}

	if got := evaluateWithProfile("v1.0.0", mk("v1.1.0"), portable); !got.Available {
		t.Error("v1.0.0 -> v1.1.0 should be available")
	}
	if got := evaluateWithProfile("v1.1.0", mk("v1.1.0"), portable); got.Available {
		t.Error("same version should not be available")
	}
	if got := evaluateWithProfile("v1.2.0", mk("v1.1.0"), portable); got.Available {
		t.Error("newer-than-manifest should not be available")
	}
	// A dev build must never auto-prompt, even against a real release.
	if got := evaluateWithProfile("dev", mk("v1.1.0"), portable); got.Available {
		t.Error("dev build should not prompt to update")
	}
	// An invalid manifest version is a check error, not an update.
	got := evaluateWithProfile("v1.0.0", mk("not-a-version"), portable)
	if got.Available || got.Err == "" {
		t.Errorf("invalid manifest version: got %+v", got)
	}
	// Metadata carries through.
	full := evaluateWithProfile("v1.0.0", mk("v1.1.0"), portable)
	if full.Latest != "v1.1.0" || full.Notes != "notes" || full.AssetSize != 999 {
		t.Errorf("metadata not carried: %+v", full)
	}
	if full.CanSelfUpdate != (runtime.GOOS != "darwin") {
		t.Errorf("CanSelfUpdate = %v on %s", full.CanSelfUpdate, runtime.GOOS)
	}
	if full.InstallMode == "" {
		t.Error("InstallMode should be set")
	}
}

func TestEvaluateDebSelectsNativePackage(t *testing.T) {
	if runtime.GOOS == "darwin" && !canSelfUpdate() {
		// evaluateWithProfile applies the macOS signed-build gate; synthetic deb
		// profiles are only meaningful on Linux (or a notarized macOS build).
		t.Skip("deb install mode is a Linux packaging path")
	}
	m := &update.Manifest{
		Version: "v2.0.0",
		Platforms: map[string]update.Asset{
			update.CurrentPlatform(): {URL: "https://example/tarball", Size: 100, SHA256: "aa"},
		},
		NativePackages: map[string]update.Asset{
			update.CurrentPlatform(): {URL: "https://example/pkg.deb", Size: 200, SHA256: "bb"},
		},
	}
	deb := installProfile{
		Mode:          installModeDeb,
		CanSelfUpdate: true,
		RequiresElev:  true,
		ArtifactKind:  artifactKindDeb,
	}
	got := evaluateWithProfile("v1.0.0", m, deb)
	if !got.Available || got.AssetSize != 200 {
		t.Fatalf("deb evaluate should use native package size: %+v", got)
	}
	if !got.RequiresElevation || got.InstallMode != installModeDeb {
		t.Fatalf("deb flags missing: %+v", got)
	}
	// Without native_packages, deb profile becomes manual.
	m2 := &update.Manifest{
		Version:   "v2.0.0",
		Platforms: map[string]update.Asset{update.CurrentPlatform(): {Size: 100}},
	}
	adjusted := profileForManifest(deb, m2)
	got = evaluateWithProfile("v1.0.0", m2, adjusted)
	if got.CanSelfUpdate || got.InstallMode != installModeManual {
		t.Fatalf("missing native package should force manual: %+v (profile=%+v)", got, adjusted)
	}
}

func TestManualUpdateRequiredErrorPreservesReason(t *testing.T) {
	err := manualUpdateRequiredError(installProfile{ManualReason: "system update helper is unavailable"})
	if !errors.Is(err, errUpdateManualRequired) {
		t.Fatalf("error = %v, want manual-update sentinel", err)
	}
	if !strings.Contains(err.Error(), "system update helper is unavailable") {
		t.Fatalf("error = %q, want profile reason", err)
	}
}

func TestLegacyChannelsSelectOfficialPointers(t *testing.T) {
	stable := manifestEndpoints("stable")
	preview := manifestEndpoints("preview")
	want := []string{
		r2Base + "/latest/latest.json",
		releaseGatewayBase + "/stable/latest.json",
		githubManifestFallback,
	}
	if !reflect.DeepEqual(stable, want) || !reflect.DeepEqual(preview, want) {
		t.Fatalf("manifest endpoints: stable=%q preview=%q want=%q", stable, preview, want)
	}
	if got := downloadPage("preview"); got != "https://reasonix.io/?download=desktop#start" {
		t.Errorf("legacy preview download page = %q", got)
	}
	if got := manifestDownloadPage("preview", "https://reasonix.io/?channel=preview&download=desktop#start"); got != "https://reasonix.io/?download=desktop#start" {
		t.Errorf("manifest official page = %q", got)
	}
	if got := manifestDownloadPage("preview", "https://example.com/releases"); got != "https://example.com/releases" {
		t.Errorf("external manifest download page = %q, want unchanged", got)
	}
	for _, unsafe := range []string{
		"javascript:alert(1)",
		"http://reasonix.io/#start",
		"https://user@reasonix.io/#start",
	} {
		if got := manifestDownloadPage("preview", unsafe); got != downloadPage("stable") {
			t.Errorf("unsafe manifest page %q = %q, want official fallback", unsafe, got)
		}
	}
}

func TestManifestChannelValidation(t *testing.T) {
	tests := []struct {
		name      string
		channel   string
		version   string
		wantError bool
	}{
		{name: "stable release", channel: "stable", version: "v1.17.21"},
		{name: "legacy preview selects official", channel: "preview", version: "v1.17.21"},
		{name: "legacy canary selects official", channel: "canary", version: "v1.17.21"},
		{name: "legacy preview rejects prerelease", channel: "preview", version: "v1.18.0-preview.7", wantError: true},
		{name: "legacy canary rejects prerelease", channel: "canary", version: "v1.17.21-canary.56", wantError: true},
		{name: "Stable rejects Preview", channel: "stable", version: "v1.18.0-preview.7", wantError: true},
		{name: "Stable requires v prefix", channel: "stable", version: "1.17.21", wantError: true},
		{name: "Stable rejects build metadata", channel: "stable", version: "v1.17.21+build.1", wantError: true},
		{name: "Stable rejects prerelease", channel: "stable", version: "v1.17.21-rc.1", wantError: true},
		{name: "invalid version", channel: "preview", version: "dev", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateManifestChannel(tt.channel, &update.Manifest{Version: tt.version})
			if (err != nil) != tt.wantError {
				t.Fatalf("validateManifestChannel(%q, %q) error = %v, wantError=%v", tt.channel, tt.version, err, tt.wantError)
			}
		})
	}
}

func validDesktopManifest(t *testing.T, selected, manifestVersion string) update.Manifest {
	t.Helper()
	tag := desktopReleaseTag(selected, manifestVersion)
	manifest := update.Manifest{
		Version:        manifestVersion,
		DownloadPage:   manifestDownloadPageURL,
		Platforms:      map[string]update.Asset{},
		NativePackages: map[string]update.Asset{},
		Downloads:      map[string]update.Asset{},
	}
	requiredAssets := append([]requiredDesktopAsset(nil), requiredDesktopUpdaterAssets...)
	requiredAssets = append(requiredAssets, requiredDesktopDownloadAssets...)
	for _, required := range requiredAssets {
		assetURL := fmt.Sprintf("%s/%s/%s", r2Base, tag, required.filename)
		asset := update.Asset{
			URL:    assetURL,
			Sig:    assetURL + ".minisig",
			Size:   1024,
			SHA256: strings.Repeat("a", 64),
		}
		switch required.group {
		case "platforms":
			manifest.Platforms[required.key] = asset
		case "native_packages":
			manifest.NativePackages[required.key] = asset
		case "downloads":
			manifest.Downloads[required.key] = asset
		}
	}
	return manifest
}

func TestDesktopManifestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*update.Manifest)
	}{
		{
			name: "missing required platform asset",
			mutate: func(m *update.Manifest) {
				delete(m.Platforms, "darwin-arm64")
			},
		},
		{
			name: "wrong filename",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.URL = strings.Replace(asset.URL, "Reasonix-", "Other-", 1)
				asset.Sig = asset.URL + ".minisig"
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "HTTP asset URL",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.URL = strings.Replace(asset.URL, "https://", "http://", 1)
				asset.Sig = asset.URL + ".minisig"
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "asset URL userinfo",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.URL = strings.Replace(asset.URL, "https://", "https://user@", 1)
				asset.Sig = asset.URL + ".minisig"
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "wrong asset host",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.URL = strings.Replace(asset.URL, "dl.reasonix.io", "example.com", 1)
				asset.Sig = asset.URL + ".minisig"
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "wrong release tag",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.URL = strings.Replace(asset.URL, desktopReleaseTag("stable", m.Version), "desktop-v9.9.9", 1)
				asset.Sig = asset.URL + ".minisig"
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "signature is not exact URL suffix",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.Sig = asset.URL + ".sig"
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "zero size",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.Size = 0
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "negative size",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.Size = -1
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "size above release maximum",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.Size = maxDesktopReleaseAssetSize + 1
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "uppercase SHA",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.SHA256 = strings.Repeat("A", 64)
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "short SHA",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.SHA256 = strings.Repeat("a", 63)
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "nonhex SHA",
			mutate: func(m *update.Manifest) {
				asset := m.Platforms["darwin-arm64"]
				asset.SHA256 = strings.Repeat("g", 64)
				m.Platforms["darwin-arm64"] = asset
			},
		},
		{
			name: "missing download page",
			mutate: func(m *update.Manifest) {
				m.DownloadPage = ""
			},
		},
		{
			name: "wrong download page",
			mutate: func(m *update.Manifest) {
				m.DownloadPage = "https://reasonix.io/?channel=stable&download=desktop#start"
			},
		},
	}

	if err := validateDesktopManifest("stable", ptr(validDesktopManifest(t, "stable", "v1.18.0"))); err != nil {
		t.Fatalf("valid Stable manifest: %v", err)
	}
	if err := validateDesktopManifest("preview", ptr(validDesktopManifest(t, "stable", "v1.19.0"))); err != nil {
		t.Fatalf("legacy Preview selection did not accept official manifest: %v", err)
	}
	t.Run("legacy manifests remain upgradeable", func(t *testing.T) {
		stable := validDesktopManifest(t, "stable", "v1.17.21")
		stable.Downloads = nil
		if err := validateDesktopManifest("stable", &stable); err != nil {
			t.Fatalf("legacy Stable manifest: %v", err)
		}
	})
	t.Run("empty downloads is not a legacy manifest", func(t *testing.T) {
		manifest := validDesktopManifest(t, "stable", "v1.17.21")
		manifest.Downloads = map[string]update.Asset{}
		if err := validateDesktopManifest("stable", &manifest); err == nil {
			t.Fatal("manifest with empty downloads bypassed the new-format asset requirements")
		}
	})
	t.Run("official manifest rejects legacy rolling asset base", func(t *testing.T) {
		manifest := validDesktopManifest(t, "stable", "v1.19.0")
		immutableBase := r2Base + "/desktop-v1.19.0/"
		rollingBase := r2Base + "/desktop-preview/"
		for key, asset := range manifest.Platforms {
			asset.URL = strings.Replace(asset.URL, immutableBase, rollingBase, 1)
			asset.Sig = asset.URL + ".minisig"
			manifest.Platforms[key] = asset
		}
		for key, asset := range manifest.NativePackages {
			asset.URL = strings.Replace(asset.URL, immutableBase, rollingBase, 1)
			asset.Sig = asset.URL + ".minisig"
			manifest.NativePackages[key] = asset
		}
		for key, asset := range manifest.Downloads {
			asset.URL = strings.Replace(asset.URL, immutableBase, rollingBase, 1)
			asset.Sig = asset.URL + ".minisig"
			manifest.Downloads[key] = asset
		}
		if err := validateDesktopManifest("stable", &manifest); err == nil {
			t.Fatal("official manifest accepted mutable rolling assets")
		}
	})
	t.Run("unified GitHub release base", func(t *testing.T) {
		manifest := validDesktopManifest(t, "stable", "v1.19.0")
		oldBase := r2Base + "/desktop-v1.19.0/"
		newBase := "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v1.19.0/"
		for key, asset := range manifest.Platforms {
			asset.URL = strings.Replace(asset.URL, oldBase, newBase, 1)
			asset.Sig = asset.URL + ".minisig"
			manifest.Platforms[key] = asset
		}
		for key, asset := range manifest.NativePackages {
			asset.URL = strings.Replace(asset.URL, oldBase, newBase, 1)
			asset.Sig = asset.URL + ".minisig"
			manifest.NativePackages[key] = asset
		}
		for key, asset := range manifest.Downloads {
			asset.URL = strings.Replace(asset.URL, oldBase, newBase, 1)
			asset.Sig = asset.URL + ".minisig"
			manifest.Downloads[key] = asset
		}
		if err := validateDesktopManifest("stable", &manifest); err != nil {
			t.Fatalf("unified GitHub release manifest: %v", err)
		}
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := validDesktopManifest(t, "stable", "v1.18.0")
			tt.mutate(&manifest)
			if err := validateDesktopManifest("stable", &manifest); err == nil {
				t.Fatal("validateDesktopManifest accepted malformed manifest")
			}
		})
	}

	t.Run("invalid native package", func(t *testing.T) {
		manifest := validDesktopManifest(t, "stable", "v1.18.0")
		native := manifest.NativePackages["linux-amd64"]
		native.Sig = native.URL + ".sig"
		manifest.NativePackages["linux-amd64"] = native
		if err := validateDesktopManifest("stable", &manifest); err == nil {
			t.Fatal("validateDesktopManifest accepted malformed native package")
		}
	})

	t.Run("mixed official bases", func(t *testing.T) {
		manifest := validDesktopManifest(t, "stable", "v1.18.0")
		asset := manifest.Platforms["darwin-arm64"]
		asset.URL = strings.Replace(
			asset.URL,
			r2Base+"/desktop-v1.18.0/",
			"https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-v1.18.0/",
			1,
		)
		asset.Sig = asset.URL + ".minisig"
		manifest.Platforms["darwin-arm64"] = asset
		if err := validateDesktopManifest("stable", &manifest); err == nil {
			t.Fatal("validateDesktopManifest accepted mixed R2 and GitHub asset bases")
		}
	})
}

func ptr[T any](value T) *T {
	return &value
}

func TestFetchManifestSkipsPrereleaseForLegacyPreviewSelection(t *testing.T) {
	var calls []string
	client := &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.String())
		version := "v1.18.0-preview.7"
		if strings.Contains(req.URL.Path, "/stable/") {
			version = "v1.18.0"
		}
		manifest := validDesktopManifest(t, "stable", version)
		body, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	manifest, err := fetchManifest(context.Background(), client, nil, "preview")
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if manifest.Version != "v1.18.0" {
		t.Fatalf("version = %q, want official fallback manifest", manifest.Version)
	}
	if len(calls) != 2 || !strings.Contains(calls[0], "/latest/") || !strings.Contains(calls[1], "/stable/") {
		t.Fatalf("endpoint calls = %q, want official latest then gateway fallback", calls)
	}
}

func TestFetchManifestSkipsMalformedSuccessfulResponse(t *testing.T) {
	var calls []string
	client := &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.String())
		manifest := validDesktopManifest(t, "stable", "v1.18.0")
		if strings.Contains(req.URL.Path, "/latest/") {
			delete(manifest.Platforms, update.CurrentPlatform())
		}
		body, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	manifest, err := fetchManifest(context.Background(), client, nil, "preview")
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if manifest.Version != "v1.18.0" {
		t.Fatalf("version = %q, want valid fallback manifest", manifest.Version)
	}
	if len(calls) != 2 || !strings.Contains(calls[0], "/latest/") || !strings.Contains(calls[1], "/stable/") {
		t.Fatalf("endpoint calls = %q, want malformed 200 to fall through", calls)
	}
}

func TestValidateUpdateRedirect(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		wantError bool
	}{
		{name: "Reasonix first-party redirect", target: "https://dl.reasonix.io/file"},
		{name: "GitHub redirect", target: "https://github.com/file"},
		{name: "GitHub HTTPS asset redirect", target: "https://release-assets.githubusercontent.com/file"},
		{name: "HTTPS downgrade", target: "http://release-assets.githubusercontent.com/file", wantError: true},
		{name: "userinfo", target: "https://user@release-assets.githubusercontent.com/file", wantError: true},
		{name: "missing hostname", target: "https:///file", wantError: true},
		{name: "arbitrary HTTPS host", target: "https://example.com/file", wantError: true},
		{name: "Reasonix suffix spoof", target: "https://dl.reasonix.io.evil.invalid/file", wantError: true},
		{name: "GitHub suffix spoof", target: "https://release-assets.githubusercontent.com.evil.invalid/file", wantError: true},
		{name: "explicit port", target: "https://dl.reasonix.io:443/file", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tt.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = validateUpdateRedirect(req, nil)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateUpdateRedirect(%q) error = %v, wantError=%v", tt.target, err, tt.wantError)
			}
		})
	}
	t.Run("redirect limit", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/file", nil)
		if err != nil {
			t.Fatal(err)
		}
		via := make([]*http.Request, 10)
		if err := validateUpdateRedirect(req, via); err == nil {
			t.Fatal("validateUpdateRedirect accepted more than 10 redirects")
		}
	})
}

func withUpdateCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	restore := updateCacheBaseDir
	updateCacheBaseDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { updateCacheBaseDir = restore })
	return dir
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestSaveCachedUpdateMarksEvaluateDownloaded(t *testing.T) {
	withUpdateCacheDir(t)
	oldChannel := channel
	channel = "stable"
	t.Cleanup(func() { channel = oldChannel })

	data := []byte("verified artifact")
	asset := update.Asset{
		URL:    "https://dl.reasonix.io/desktop-v9.9.9/Reasonix-linux-amd64.tar.gz",
		Size:   int64(len(data)),
		SHA256: sha256Hex(data),
	}
	manifest := &update.Manifest{
		Version:   "v9.9.9",
		Platforms: map[string]update.Asset{update.CurrentPlatform(): asset},
	}
	portable := installProfile{Mode: installModePortable, CanSelfUpdate: true, ArtifactKind: artifactKindTarball}
	if got := evaluateWithProfile("v1.0.0", manifest, portable); got.Downloaded {
		t.Fatal("fresh cache should not report a downloaded update")
	}
	meta, err := saveCachedUpdate("v9.9.9", asset, data, artifactKindTarball, nil)
	if err != nil {
		t.Fatalf("saveCachedUpdate: %v", err)
	}
	if meta.Version != "v9.9.9" || meta.Channel != "stable" || meta.Platform != update.CurrentPlatform() {
		t.Fatalf("cached metadata mismatch: %+v", meta)
	}
	if got := evaluateWithProfile("v1.0.0", manifest, portable); !got.Downloaded {
		t.Fatalf("evaluate did not detect cached update: %+v", got)
	}
}

func TestCachedUpdateRejectsTamperedArtifact(t *testing.T) {
	withUpdateCacheDir(t)
	oldChannel := channel
	channel = "stable"
	t.Cleanup(func() { channel = oldChannel })

	data := []byte("verified artifact")
	asset := update.Asset{
		URL:    "https://dl.reasonix.io/desktop-v9.9.9/Reasonix-linux-amd64.tar.gz",
		Size:   int64(len(data)),
		SHA256: sha256Hex(data),
	}
	meta, err := saveCachedUpdate("v9.9.9", asset, data, artifactKindTarball, nil)
	if err != nil {
		t.Fatalf("saveCachedUpdate: %v", err)
	}
	if err := os.WriteFile(meta.Path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cachedUpdateMatches("v9.9.9", asset, artifactKindTarball) {
		t.Fatal("tampered cached artifact should not match")
	}
	if _, _, err := readVerifiedCachedUpdate(); err == nil {
		t.Fatal("readVerifiedCachedUpdate should reject a tampered artifact")
	}
}

func TestCachedUpdateAcceptsLegacyChannelAlias(t *testing.T) {
	withUpdateCacheDir(t)

	data := []byte("verified artifact")
	asset := update.Asset{
		URL:    "https://dl.reasonix.io/desktop-v9.9.9/Reasonix-linux-amd64.tar.gz",
		Size:   int64(len(data)),
		SHA256: sha256Hex(data),
	}
	if _, err := saveCachedUpdateForChannel("stable", "v9.9.9", asset, data, artifactKindTarball, nil); err != nil {
		t.Fatalf("saveCachedUpdateForChannel: %v", err)
	}
	if _, _, err := readVerifiedCachedUpdateForChannel("preview"); err != nil {
		t.Fatalf("legacy Preview alias did not read official cache: %v", err)
	}
}

func TestLegacyChannelAliasDoesNotPermitDowngrade(t *testing.T) {
	oldChannel := channel
	channel = "preview"
	t.Cleanup(func() { channel = oldChannel })

	m := &update.Manifest{
		Version: "v1.6.0",
		Platforms: map[string]update.Asset{
			update.CurrentPlatform(): {Size: 100},
		},
	}
	got := evaluateWithProfileForChannel(
		"v1.7.0-preview.12",
		"stable",
		m,
		installProfile{Mode: installModePortable, CanSelfUpdate: true},
	)
	if got.Available {
		t.Fatalf("legacy Preview alias permitted an official downgrade: %+v", got)
	}
	if got.Channel != "stable" {
		t.Fatalf("channel = %q, want stable", got.Channel)
	}
}

func TestDebCacheRequiresSignatureAndRejectsTarballReuse(t *testing.T) {
	withUpdateCacheDir(t)
	oldChannel := channel
	channel = "stable"
	t.Cleanup(func() { channel = oldChannel })

	data := []byte("deb-bytes")
	asset := update.Asset{
		URL:    "https://dl.reasonix.io/desktop-v9.9.9/Reasonix-linux-amd64.deb",
		Size:   int64(len(data)),
		SHA256: sha256Hex(data),
	}
	if _, err := saveCachedUpdate("v9.9.9", asset, data, artifactKindDeb, nil); err == nil {
		t.Fatal("deb cache without signature must fail")
	}
	sig := []byte("minisig-bytes")
	meta, err := saveCachedUpdate("v9.9.9", asset, data, artifactKindDeb, sig)
	if err != nil {
		t.Fatalf("saveCachedUpdate deb: %v", err)
	}
	if meta.SignaturePath == "" {
		t.Fatal("deb cache must record signature path")
	}
	if !cachedUpdateMatches("v9.9.9", asset, artifactKindDeb) {
		t.Fatal("deb cache with matching signature should match")
	}
	// A tarball install must not reuse a deb cache.
	if cachedUpdateMatches("v9.9.9", asset, artifactKindTarball) {
		t.Fatal("deb cache must not match tarball requests")
	}
	// Signature removal invalidates the download marker.
	if err := os.Remove(meta.SignaturePath); err != nil {
		t.Fatal(err)
	}
	if cachedUpdateMatches("v9.9.9", asset, artifactKindDeb) {
		t.Fatal("deb cache without signature file must not match")
	}

	// Portable legacy cache (no artifactKind) remains valid for tarball.
	tarball := []byte("tarball-bytes")
	tAsset := update.Asset{
		URL:    "https://dl.reasonix.io/desktop-v9.9.9/Reasonix-linux-amd64.tar.gz",
		Size:   int64(len(tarball)),
		SHA256: sha256Hex(tarball),
	}
	meta, err = saveCachedUpdate("v9.9.9", tAsset, tarball, artifactKindTarball, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate pre-artifactKind metadata.
	meta.ArtifactKind = ""
	raw, _ := json.MarshalIndent(meta, "", "  ")
	path, _ := updateMetadataPath()
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if !cachedUpdateMatches("v9.9.9", tAsset, artifactKindTarball) {
		t.Fatal("legacy portable cache should still match tarball")
	}
	if cachedUpdateMatches("v9.9.9", tAsset, artifactKindDeb) {
		t.Fatal("legacy portable cache must not match deb")
	}
}

func TestProfileForManifestDebWithoutHelperBecomesManual(t *testing.T) {
	// profileForManifest checks linuxDebHelperReady(); on non-linux it is always
	// false, so a synthetic deb profile without native assets becomes manual.
	base := installProfile{Mode: installModeDeb, CanSelfUpdate: true, RequiresElev: true, ArtifactKind: artifactKindDeb}
	m := &update.Manifest{Version: "v1.0.0"}
	got := profileForManifest(base, m)
	if got.Mode != installModeManual || got.CanSelfUpdate {
		t.Fatalf("expected manual without native package: %+v", got)
	}
}

func TestCheckSHA256(t *testing.T) {
	data := []byte("hello world")
	// echo -n "hello world" | shasum -a 256
	const sum = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if err := checkSHA256(data, sum); err != nil {
		t.Errorf("matching digest should pass: %v", err)
	}
	if err := checkSHA256(data, "deadbeef"); err == nil {
		t.Error("mismatched digest should fail")
	}
	// Case-insensitive hex.
	if err := checkSHA256(data, "B94D27B9934D3E08A52E52D7DA7DABFAC484EFE37A5380EE9088F7ACE2EFCDE9"); err != nil {
		t.Errorf("uppercase digest should pass: %v", err)
	}
}

func TestExtractBinary(t *testing.T) {
	want := []byte("#!/bin/sh\necho reasonix\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string][]byte{"README": []byte("ignore me"), "reasonix-desktop": want}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()

	got, err := extractBinary(buf.Bytes(), "reasonix-desktop")
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted %q, want %q", got, want)
	}
	if _, err := extractBinary(buf.Bytes(), "missing"); err == nil {
		t.Error("missing entry should error")
	}
}

func TestExtractLinuxReleaseUnitRejectsAmbiguousMembers(t *testing.T) {
	makeArchive := func(t *testing.T, headers []tar.Header, bodies [][]byte) []byte {
		t.Helper()
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for i, header := range headers {
			if err := tw.WriteHeader(&header); err != nil {
				t.Fatal(err)
			}
			if i < len(bodies) {
				if _, err := tw.Write(bodies[i]); err != nil {
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
	base := func(name string, body []byte) tar.Header {
		return tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
	}
	headers := []tar.Header{
		base("reasonix-desktop", []byte("desktop")),
		base("reasonix-guard", []byte("guard")),
		base("reasonix", []byte("cli")),
	}
	bodies := [][]byte{[]byte("desktop"), []byte("guard"), []byte("cli")}
	if got, err := extractLinuxReleaseUnit(makeArchive(t, headers, bodies)); err != nil ||
		string(got["reasonix-desktop"]) != "desktop" {
		t.Fatalf("complete release extraction = %v, %q", err, got["reasonix-desktop"])
	}

	duplicateHeaders := append(append([]tar.Header(nil), headers...), base("nested/reasonix", []byte("duplicate")))
	duplicateBodies := append(append([][]byte(nil), bodies...), []byte("duplicate"))
	if _, err := extractLinuxReleaseUnit(makeArchive(t, duplicateHeaders, duplicateBodies)); err == nil ||
		!strings.Contains(err.Error(), "appears more than once") {
		t.Fatalf("duplicate release member error = %v", err)
	}

	nonRegular := append([]tar.Header(nil), headers...)
	nonRegular[1] = tar.Header{Name: "reasonix-guard", Typeflag: tar.TypeSymlink, Linkname: "outside"}
	nonRegularBodies := [][]byte{bodies[0], nil, bodies[2]}
	if _, err := extractLinuxReleaseUnit(makeArchive(t, nonRegular, nonRegularBodies)); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("non-regular release member error = %v", err)
	}
}

func TestApplyLinuxVersionedActivatesWithoutPersistingGuard(t *testing.T) {
	root := robustTempDir(t)
	source := robustTempDir(t)
	for _, name := range []string{installlayout.DesktopBinaryName(), installlayout.CLIBinaryName()} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("old-"+name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installlayout.ActivateVersion(installlayout.ActivationRequest{
		InstallRoot: root,
		Version:     "v1.20.0",
		RequestID:   "seed-linux",
		Members: []installlayout.Member{
			{Name: installlayout.DesktopBinaryName(), Path: filepath.Join(source, installlayout.DesktopBinaryName())},
			{Name: installlayout.CLIBinaryName(), Path: filepath.Join(source, installlayout.CLIBinaryName())},
		},
		RequiredNames: []string{installlayout.DesktopBinaryName(), installlayout.CLIBinaryName()},
	}); err != nil {
		t.Fatal(err)
	}

	originalRoot := currentInstallDirForLinuxUpdate
	currentInstallDirForLinuxUpdate = func() string { return root }
	t.Cleanup(func() { currentInstallDirForLinuxUpdate = originalRoot })

	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"reasonix-desktop", "reasonix-guard", "reasonix"} {
		body := []byte("new-" + name)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	if err := applyLinuxVersioned(archive.Bytes(), "1.20.1"); err != nil {
		t.Fatal(err)
	}
	ptr, err := installlayout.ReadCurrent(root)
	if err != nil || ptr.ActiveVersion != "v1.20.1" {
		t.Fatalf("pointer=%+v err=%v", ptr, err)
	}
	activeDesktop, err := installlayout.ActiveDesktopPath(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(activeDesktop)
	if err != nil || string(data) != "new-reasonix-desktop" {
		t.Fatalf("active desktop=%q err=%v", data, err)
	}
	for _, guardPath := range []string{
		filepath.Join(root, "reasonix-guard"),
		filepath.Join(root, "versions", "v1.20.1", "reasonix-guard"),
	} {
		if _, err := os.Lstat(guardPath); !os.IsNotExist(err) {
			t.Fatalf("Guard persisted at %s: %v", guardPath, err)
		}
	}
}

func TestApplyLinuxHoldsReleaseUnitLockDuringReplace(t *testing.T) {
	dir := robustTempDir(t)
	t.Setenv("REASONIX_HOME", robustTempDir(t))
	exe := filepath.Join(dir, "reasonix-desktop")
	releasePaths := releaseUnitPathsFor(dir, "linux")
	for _, path := range releasePaths {
		if err := os.WriteFile(path, []byte("old"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	prepared, err := repair.PrepareFileUpdate("v1", "v2", exe, releasePaths[1:]...)
	if err != nil {
		t.Fatal(err)
	}
	originalPath := currentExecutablePathForLinux
	originalApply := applyLinuxReleaseUnit
	currentExecutablePathForLinux = func() string { return exe }
	entered := make(chan struct{})
	releaseReplace := make(chan struct{})
	applyLinuxReleaseUnit = func(
		tx *repair.UpdateTransaction,
		exe string,
		bin, guard, cli []byte,
	) ([]repair.FileUpdateInstallReceipt, error) {
		close(entered)
		<-releaseReplace
		return originalApply(tx, exe, bin, guard, cli)
	}
	t.Cleanup(func() {
		currentExecutablePathForLinux = originalPath
		applyLinuxReleaseUnit = originalApply
	})

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"reasonix-desktop", "reasonix-guard", "reasonix"} {
		body := []byte(name)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	applyDone := make(chan error, 1)
	go func() { applyDone <- applyLinux(buf.Bytes(), prepared) }()
	select {
	case <-entered:
	case err := <-applyDone:
		t.Fatalf("applyLinux failed before replacement: %v", err)
	}

	lockDone := make(chan error, 1)
	go func() {
		unlock, err := repair.LockRepairMutations(releasePaths...)
		if err == nil {
			unlock()
		}
		lockDone <- err
	}()
	select {
	case err := <-lockDone:
		close(releaseReplace)
		t.Fatalf("competing updater lock acquired during Linux replacement: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(releaseReplace)
	if err := <-applyDone; err != nil {
		t.Fatalf("applyLinux: %v", err)
	}
	if err := <-lockDone; err != nil {
		t.Fatalf("competing lock after replacement: %v", err)
	}
	if _, ok := repair.ReadUpdateApplyFailure(); ok {
		t.Fatal("successful Linux release-unit publish left an interruption marker")
	}
}

func fastRetry(t *testing.T) {
	t.Helper()
	restore := retryBackoff
	retryBackoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryBackoff = restore })
}

func TestDownloadRecoversFromMidStreamReset(t *testing.T) {
	fastRetry(t)
	const body = "complete-installer-bytes"
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < int32(downloadAttempts) {
			// Mid-stream reset: promise 100 bytes, send a few, drop the socket —
			// the client's body read fails with unexpected EOF, exactly the CN-IPv6
			// "forcibly closed" case the retry exists for.
			conn, bw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			bw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\npartial")
			bw.Flush()
			conn.Close()
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	data, err := download(context.Background(), srv.Client(), nil, srv.URL, 0, nil)
	if err != nil {
		t.Fatalf("download should recover after %d resets: %v", downloadAttempts-1, err)
	}
	if string(data) != body {
		t.Fatalf("got %q, want %q", data, body)
	}
	if n := calls.Load(); n != int32(downloadAttempts) {
		t.Fatalf("made %d attempts, want %d", n, downloadAttempts)
	}
}

func TestDownloadGivesUpAfterCap(t *testing.T) {
	fastRetry(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	if _, err := download(context.Background(), srv.Client(), nil, srv.URL, 0, nil); err == nil {
		t.Fatal("download should fail after exhausting retries")
	}
	if n := calls.Load(); n != int32(downloadAttempts) {
		t.Fatalf("made %d attempts, want %d", n, downloadAttempts)
	}
}

func TestRetryTransientStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	if err := retryTransient(ctx, func(int) error {
		calls++
		return errors.New("boom")
	}); err == nil {
		t.Fatal("cancelled retry should return the error")
	}
	if calls != 1 {
		t.Fatalf("cancelled retry made %d calls, want 1", calls)
	}
}

func TestDownloadResumesWithRange(t *testing.T) {
	fastRetry(t)
	full := bytes.Repeat([]byte("0123456789"), 50) // 500 bytes
	const cut = 200
	var calls atomic.Int32
	rangeCh := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// First attempt: promise the whole file, send a prefix, drop the socket.
			conn, bw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			fmt.Fprintf(bw, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(full))
			bw.Write(full[:cut])
			bw.Flush()
			conn.Close()
			return
		}
		// Resume attempt: honor the Range header with a 206 + Content-Range.
		rng := r.Header.Get("Range")
		rangeCh <- rng
		start := 0
		fmt.Sscanf(rng, "bytes=%d-", &start)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(full)-1, len(full)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(full[start:])
	}))
	defer srv.Close()

	data, err := download(context.Background(), srv.Client(), nil, srv.URL, 0, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(data, full) {
		t.Fatalf("assembled %d bytes, want %d (equal=%v)", len(data), len(full), bytes.Equal(data, full))
	}
	select {
	case rng := <-rangeCh:
		if rng != fmt.Sprintf("bytes=%d-", cut) {
			t.Fatalf("resume Range = %q, want bytes=%d-", rng, cut)
		}
	default:
		t.Fatal("resume attempt sent no Range header")
	}
}

func TestDownloadFallsBackToSecondClient(t *testing.T) {
	fastRetry(t)
	const body = "served-over-ipv4"
	primary := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset (ipv6)")
	})}
	var fbCalls atomic.Int32
	fallback := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		fbCalls.Add(1)
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			Header:        make(http.Header),
		}, nil
	})}

	data, err := download(context.Background(), primary, fallback, "http://example.invalid/x", 0, nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if string(data) != body {
		t.Fatalf("got %q, want %q", data, body)
	}
	if fbCalls.Load() == 0 {
		t.Fatal("fallback client was never used after the primary failed")
	}
}

func TestDownloadRejectsBodyShorterThanManifestSize(t *testing.T) {
	fastRetry(t)
	body := []byte("short")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	if _, err := download(context.Background(), srv.Client(), nil, srv.URL, int64(len(body)+1), nil); err == nil {
		t.Fatal("download accepted fewer bytes than the manifest declared")
	}
}

func TestDownloadRejectsBodyLongerThanManifestSize(t *testing.T) {
	fastRetry(t)
	body := bytes.Repeat([]byte("x"), 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	if _, err := download(context.Background(), srv.Client(), nil, srv.URL, 8, nil); err == nil {
		t.Fatal("download accepted more bytes than the manifest declared")
	}
}

func TestFetchBytesFallsBackToSecondClient(t *testing.T) {
	fastRetry(t)
	primary := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("read tcp [ipv6]: connection reset")
	})}
	fallback := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("manifest")),
			Header:     make(http.Header),
		}, nil
	})}

	data, err := fetchBytesFallback(context.Background(), primary, fallback, "https://example.invalid/latest.json")
	if err != nil {
		t.Fatalf("fetchBytesFallback: %v", err)
	}
	if string(data) != "manifest" {
		t.Fatalf("got %q, want manifest", data)
	}
}

func TestFetchBytesFallbackEscapesStalledPrimary(t *testing.T) {
	fastRetry(t)
	originalTimeout := fetchAttemptTimeout
	fetchAttemptTimeout = 10 * time.Millisecond
	t.Cleanup(func() { fetchAttemptTimeout = originalTimeout })
	primary := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	fallback := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("ipv4")),
			Header:     make(http.Header),
		}, nil
	})}

	data, err := fetchBytesFallback(context.Background(), primary, fallback, "https://example.invalid/latest.json")
	if err != nil {
		t.Fatalf("fetchBytesFallback: %v", err)
	}
	if string(data) != "ipv4" {
		t.Fatalf("got %q, want ipv4", data)
	}
}

func TestFetchBytesDoesNotRetryPermanentHTTPStatus(t *testing.T) {
	fastRetry(t)
	var calls atomic.Int32
	client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Header:     make(http.Header),
		}, nil
	})}

	if _, err := fetchBytes(context.Background(), client, "https://example.invalid/latest.json"); err == nil {
		t.Fatal("fetchBytes should return a permanent HTTP error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("permanent HTTP error made %d requests, want 1", got)
	}
}

func TestFetchBytesRejectsOversizeResponsesWithoutRetry(t *testing.T) {
	fastRetry(t)
	t.Run("declared content length", func(t *testing.T) {
		var calls atomic.Int32
		client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				ContentLength: 9,
				Body:          io.NopCloser(strings.NewReader("ignored")),
				Header:        make(http.Header),
			}, nil
		})}
		if _, err := fetchBytesFallbackForChannelSized(
			context.Background(),
			client,
			nil,
			"stable",
			"https://example.invalid/latest.json",
			8,
		); !errors.Is(err, errUpdateResponseTooLarge) {
			t.Fatalf("declared oversize error = %v, want errUpdateResponseTooLarge", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("declared oversize response made %d requests, want 1", got)
		}
	})

	t.Run("chunked body", func(t *testing.T) {
		client := &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				ContentLength: -1,
				Body:          io.NopCloser(strings.NewReader("123456789")),
				Header:        make(http.Header),
			}, nil
		})}
		if _, err := fetchBytesFallbackForChannelSized(
			context.Background(),
			client,
			nil,
			"stable",
			"https://example.invalid/latest.json",
			8,
		); !errors.Is(err, errUpdateResponseTooLarge) {
			t.Fatalf("chunked oversize error = %v, want errUpdateResponseTooLarge", err)
		}
	})
}

func TestDownloadRejectsAssetSizeAboveMaximum(t *testing.T) {
	if _, err := download(
		context.Background(),
		&http.Client{},
		nil,
		"https://dl.reasonix.io/file",
		maxDesktopReleaseAssetSize+1,
		nil,
	); err == nil {
		t.Fatal("download accepted an asset size above the release maximum")
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
