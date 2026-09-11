package main

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type signPathArtifactConfiguration struct {
	Zip signPathZip `xml:"zip-file"`
}

type signPathZip struct {
	Files    []signPathPEFile    `xml:"pe-file"`
	FileSets []signPathPEFileSet `xml:"pe-file-set"`
}

type signPathPEFile struct {
	Path   string    `xml:"path,attr"`
	Sign   *struct{} `xml:"authenticode-sign"`
	Verify *struct{} `xml:"authenticode-verify"`
}

type signPathPEFileSet struct {
	Includes []struct {
		Path       string `xml:"path,attr"`
		MinMatches string `xml:"min-matches,attr"`
	} `xml:"include"`
	ForEach struct {
		Sign   *struct{} `xml:"authenticode-sign"`
		Verify *struct{} `xml:"authenticode-verify"`
	} `xml:"for-each"`
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func parseSignPathConfiguration(t *testing.T, name string) signPathArtifactConfiguration {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", ".signpath", "artifact-configurations", name))
	if err != nil {
		t.Fatal(err)
	}
	var config signPathArtifactConfiguration
	if err := xml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return config
}

func TestWindowsReleaseSignsPayloadBeforeRepackaging(t *testing.T) {
	workflow := readTestFile(t, "../.github/workflows/release-desktop.yml")
	orderedSteps := []string{
		"name: Build and package",
		"name: Checkout protected release verifier",
		"name: Smoke-test packaged Electron startup",
		"name: Upload unsigned Windows payload for SignPath",
		"name: Submit Windows payload for Authenticode signing",
		"name: Approve and download signed Windows payload",
		"name: Bind signed Windows payload to release manifest",
		"name: Rebuild Windows packages from signed payload",
		"name: Upload unsigned installer for SignPath",
		"name: Submit installer for Authenticode signing",
		"name: Approve and download signed Windows installer",
		"name: Replace installer with signed build",
		"name: Verify Windows Authenticode release contract",
		"name: Sign artifacts (minisign)",
	}
	last := -1
	for _, step := range orderedSteps {
		index := strings.Index(workflow, step)
		if index < 0 {
			t.Fatalf("desktop release workflow is missing %q", step)
		}
		if index <= last {
			t.Fatalf("desktop release workflow step %q is out of order", step)
		}
		last = index
	}
	for _, want := range []string{
		`artifact-configuration-slug: windows-payload`,
		`artifact-configuration-slug: windows-installer-v2`,
		`path: desktop/build/windows/signing-payload`,
		`path: desktop/build/windows/installer-signing-bundle`,
		`github.repository == 'esengine/DeepSeek-Reasonix'`,
		`SIGNPATH_API_TOKEN is required for public Windows Preview and Stable releases`,
		`SIGNPATH_RELEASE_SIGNING_ATTESTATION does not match the current protected signing contract`,
		`signing-policy-slug: release-signing`,
		`(needs.build.result == 'success' || (needs.build.result == 'skipped' && inputs.preflight_artifact_prefix != '' && inputs.orchestrated && inputs.signing_preflight_verified))`,
		`needs.signing-contract.result == 'success' && needs.cache-guard.result == 'success' && !inputs.production_signing_smoke && !inputs.signing_preflight`,
		`go run ./cmd/signpath-contract fingerprint`,
		`wait-for-completion: false`,
		`steps.submit-windows-payload.outputs.signing-request-id`,
		`steps.submit-windows-installer.outputs.signing-request-id`,
		`scripts/complete-signpath-request.ps1`,
		`-WaitForExternalApproval:$waitForExternalApproval`,
		`go run ./cmd/sign windows-payload ../signed-payload "${{ needs.resolve.outputs.version }}"`,
		`go run ./cmd/sign sign ../signed-payload/reasonix-payload.json`,
		`go run ./cmd/sign verify ../signed-payload/reasonix-payload.json`,
		`REASONIX_REQUIRE_PAYLOAD_MANIFEST: "1"`,
		`ref: ${{ github.workflow_sha }}`,
		`path: release-control`,
		`node desktop/packaging/smoke.mjs`,
		`./release-control/scripts/verify-windows-authenticode.ps1`,
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("desktop release workflow is missing signing contract %q", want)
		}
	}
	ciWorkflow := readTestFile(t, "../.github/workflows/ci.yml")
	if !strings.Contains(ciWorkflow, `node packaging/smoke.mjs build/electron/windows-amd64/app`) {
		t.Error("Windows CI must smoke the packaged Electron shell startup")
	}
	if strings.Contains(ciWorkflow, "webview2") || strings.Contains(ciWorkflow, "WebView2") {
		t.Error("Windows CI must not reference the retired WebView2 smoke harness")
	}
	for _, forbidden := range []string{
		`signing-policy-slug: test-signing`,
		`artifact-configuration-slug: windows-installer-test-v2`,
		`steps.ver.outputs.channel == 'canary'`,
	} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("public desktop release workflow contains legacy Canary signing contract %q", forbidden)
		}
	}

	packager := readTestFile(t, "../scripts/package-windows-desktop.sh")
	copyMain := strings.Index(packager, `cp "$PAYLOAD/$BINNAME.exe" "$INSTALLER_DIR/$BINNAME.exe"`)
	makeNSIS := strings.Index(packager, "makensis \\\n")
	portable := strings.Index(packager, `cp "$PAYLOAD/$BINNAME.exe" "$portable_staging/versions/$version_label/$BINNAME.exe"`)
	bundle := strings.Index(packager, `installer_bundle="$DESKTOP/build/windows/installer-signing-bundle"`)
	if copyMain < 0 || makeNSIS < 0 || portable < 0 || bundle < 0 {
		t.Fatal("Windows packager is missing the signed-payload packaging stages")
	}
	if !(copyMain < makeNSIS && makeNSIS < portable && portable < bundle) {
		t.Fatalf("Windows package order must be payload copy -> NSIS -> portable -> signing bundle (copy=%d nsis=%d portable=%d bundle=%d)", copyMain, makeNSIS, portable, bundle)
	}
	for _, want := range []string{
		`node "$DESKTOP/packaging/signing-files.mjs" "$PAYLOAD" --check`,
		`cp "$PAYLOAD/$GUARDNAME.exe" "$INSTALLER_DIR/$GUARDNAME.exe"`,
		`cp "$PAYLOAD/$LAUNCHERNAME.exe" "$INSTALLER_DIR/$LAUNCHERNAME.exe"`,
		`cp "$PAYLOAD/$UPDATE_HELPER" "$INSTALLER_DIR/$UPDATE_HELPER"`,
		`cp "$PAYLOAD/$WINDOWS_CLINAME.exe" "$INSTALLER_DIR/$WINDOWS_CLINAME.exe"`,
		`cp -R "$PAYLOAD/app" "$INSTALLER_DIR/app"`,
		`rm -f -- "$INSTALLER_DIR/$PAYLOAD_MANIFEST" "$INSTALLER_DIR/$PAYLOAD_SIGNATURE"`,
		`cp "$PAYLOAD/$PAYLOAD_MANIFEST" "$INSTALLER_DIR/$PAYLOAD_MANIFEST"`,
		`cp "$PAYLOAD/$PAYLOAD_SIGNATURE" "$INSTALLER_DIR/$PAYLOAD_SIGNATURE"`,
		`REASONIX_REQUIRE_PAYLOAD_MANIFEST`,
		`"-DARG_REASONIX_SIGNED_UNINSTALLER=${uninstaller_path}"`,
		`cp "$PAYLOAD/$LAUNCHERNAME.exe" "$portable_staging/$APPNAME.exe"`,
		`cp -R "$PAYLOAD/app" "$portable_staging/versions/$version_label/app"`,
		`"$ROOT/scripts/verify-windows-portable.sh" "$portable_staging"`,
		`cp -R "$PAYLOAD/app" "$installer_bundle/app"`,
	} {
		if !strings.Contains(packager, want) {
			t.Errorf("Windows packager is missing payload contract %q", want)
		}
	}

	verifier := readTestFile(t, "../scripts/verify-windows-authenticode.ps1")
	for _, want := range []string{
		"Get-AuthenticodeSignature",
		"$signature.SignerCertificate",
		"$signature.Status -ne \"Valid\"",
		"Expand-Archive",
		`Get-ChildItem -LiteralPath $extractRoot -Recurse -File -Filter "*.exe"`,
		`$activeDir.Replace("\", "/") -ne "versions/$activeVersion"`,
		`Portable = (Join-Path $activeDir "reasonix-desktop.exe")`,
		`Portable = "Reasonix.exe"; Payload = "reasonix-launcher.exe"`,
		"6 release unit + $appExeCount Electron app tree",
		"Get-FileHash -Algorithm SHA256",
	} {
		if !strings.Contains(verifier, want) {
			t.Errorf("Windows Authenticode verifier is missing %q", want)
		}
	}

	completer := readTestFile(t, "../scripts/complete-signpath-request.ps1")
	for _, want := range []string{
		`$request.signingPolicySlug -ne $ExpectedSigningPolicySlug`,
		`$status.status -eq "WaitingForApproval"`,
		`"$requestBaseUrl/Approve"`,
		`"$requestBaseUrl/Status"`,
		`"$requestBaseUrl/SignedArtifact"`,
		`$status.status -ne "Completed"`,
		`[switch]$WaitForExternalApproval`,
		`if ($WaitForExternalApproval)`,
		`Waiting for an authorized SignPath user to approve request`,
		`OutputArtifactDirectory must resolve inside GITHUB_WORKSPACE`,
		`[string]$ApiUrl = "https://app.signpath.io/api"`,
		`Expand-Archive`,
	} {
		if !strings.Contains(completer, want) {
			t.Errorf("SignPath request completer is missing %q", want)
		}
	}
}

func TestWindowsPackagerRejectsMissingOrPartialRequiredPayloadManifest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		manifest  bool
		signature bool
		want      string
	}{
		{name: "missing", want: "signed Windows packaging requires"},
		{name: "manifest only", manifest: true, want: "must be provided together"},
		{name: "signature only", signature: true, want: "must be provided together"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := t.TempDir()
			for _, name := range []string{
				"reasonix-desktop.exe",
				"reasonix-guard.exe",
				"reasonix-launcher.exe",
				"reasonix-update-helper.exe",
				"reasonix-cli.exe",
				"reasonix-uninstall.exe",
			} {
				if err := os.WriteFile(filepath.Join(payload, name), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// The packager validates the Electron app/ tree and signing-files.txt
			// before the manifest gate, so the fixture must carry both.
			if err := os.MkdirAll(filepath.Join(payload, "app"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(payload, "app", "Reasonix.exe"), []byte("shell"), 0o600); err != nil {
				t.Fatal(err)
			}
			signingList := "app/Reasonix.exe\nreasonix-cli.exe\nreasonix-desktop.exe\nreasonix-guard.exe\nreasonix-launcher.exe\nreasonix-uninstall.exe\nreasonix-update-helper.exe\n"
			if err := os.WriteFile(filepath.Join(payload, "signing-files.txt"), []byte(signingList), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.manifest {
				if err := os.WriteFile(filepath.Join(payload, "reasonix-payload.json"), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.signature {
				if err := os.WriteFile(filepath.Join(payload, "reasonix-payload.json.minisig"), []byte("sig"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "../scripts/package-windows-desktop.sh", "amd64", payload)
			cmd.Env = append(os.Environ(), "REASONIX_REQUIRE_PAYLOAD_MANIFEST=1")
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.want) {
				t.Fatalf("packager error = %v, output = %q, want %q", err, output, tc.want)
			}
		})
	}
}

func TestProductionSigningRunsOnlyFromProtectedControlPlane(t *testing.T) {
	stable := readTestFile(t, "../.github/workflows/release-stable.yml")
	desktop := readTestFile(t, "../.github/workflows/release-desktop.yml")
	if strings.Contains(stable, "\n  push:\n") || strings.Contains(desktop, "\n  push:\n") {
		t.Fatal("production workflows must not run directly with a tag-shaped SignPath origin")
	}
	for _, want := range []string{
		`ALLOW_STABLE_RECOVERY: ${{ inputs.allow_recovery }}`,
		`allow_recovery: 'false'`,
		`signing_preflight: true`,
		`signing_preflight_verified: true`,
		`needs: [authorize, signpath-preflight]`,
	} {
		if !strings.Contains(stable+"\n"+readTestFile(t, "../.github/workflows/release-stable-trigger.yml"), want) {
			t.Errorf("stable relay is missing normal-release recovery guard %q", want)
		}
	}

	for _, path := range []string{
		"../.github/workflows/release-stable-trigger.yml",
	} {
		relay := readTestFile(t, path)
		for _, want := range []string{
			`actions: write`,
			`CONTROL_PLANE_REF: ${{ github.event.repository.default_branch }}`,
			`process.env.CONTROL_PLANE_REF !== 'main-v2'`,
			`createWorkflowDispatch`,
			`ref: process.env.CONTROL_PLANE_REF`,
		} {
			if !strings.Contains(relay, want) {
				t.Errorf("%s is missing protected control-plane contract %q", path, want)
			}
		}
	}

	for _, path := range []string{
		"../.github/workflows/release-preview.yml",
		"../.github/workflows/release-cli-trigger.yml",
		"../.github/workflows/release-desktop-trigger.yml",
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("retired public prerelease workflow %s still exists or cannot be checked: %v", path, err)
		}
	}
}

func TestSignPathConfigurationsCoverExactWindowsPayload(t *testing.T) {
	flatPayload := map[string]bool{
		"reasonix-desktop.exe":       true,
		"reasonix-guard.exe":         true,
		"reasonix-launcher.exe":      true,
		"reasonix-update-helper.exe": true,
		"reasonix-cli.exe":           true,
		"reasonix-uninstall.exe":     true,
	}

	payload := parseSignPathConfiguration(t, "windows-payload.xml")
	// The signed unit is the flat Go payload plus every PE file in the Electron
	// app/ tree: Reasonix.exe is explicit, the rest ride the pe-file-set glob.
	if len(payload.Zip.Files) != len(flatPayload)+1 {
		t.Fatalf("windows-payload.xml files = %d, want %d", len(payload.Zip.Files), len(flatPayload)+1)
	}
	for _, file := range payload.Zip.Files {
		if !flatPayload[file.Path] && file.Path != "app/Reasonix.exe" {
			t.Errorf("windows-payload.xml contains unexpected path %q", file.Path)
		}
		if file.Sign == nil || file.Verify != nil {
			t.Errorf("windows-payload.xml %q must sign, not verify", file.Path)
		}
	}
	if len(payload.Zip.FileSets) != 1 {
		t.Fatalf("windows-payload.xml pe-file-sets = %d, want 1", len(payload.Zip.FileSets))
	}
	payloadSet := payload.Zip.FileSets[0]
	if payloadSet.ForEach.Sign == nil || payloadSet.ForEach.Verify != nil {
		t.Error("windows-payload.xml pe-file-set must sign every app/ PE file")
	}
	for _, want := range []string{"app/**/*.exe", "app/**/*.dll"} {
		found := false
		for _, include := range payloadSet.Includes {
			if include.Path == want && include.MinMatches == "1" {
				found = true
			}
		}
		if !found {
			t.Errorf("windows-payload.xml pe-file-set must include %s with min-matches=1", want)
		}
	}

	installer := parseSignPathConfiguration(t, "windows-installer-v2.xml")
	if len(installer.Zip.Files) != len(flatPayload)+1 {
		t.Fatalf("windows-installer.xml files = %d, want %d", len(installer.Zip.Files), len(flatPayload)+1)
	}
	verified := 0
	signedInstaller := 0
	for _, file := range installer.Zip.Files {
		switch {
		case file.Path == "*installer*.exe":
			if file.Sign == nil || file.Verify != nil {
				t.Error("windows-installer.xml must sign the outer installer")
			}
			signedInstaller++
		case flatPayload[file.Path]:
			if file.Verify == nil || file.Sign != nil {
				t.Errorf("windows-installer.xml %q must verify, not re-sign", file.Path)
			}
			verified++
		default:
			t.Errorf("windows-installer.xml contains unexpected path %q", file.Path)
		}
	}
	if signedInstaller != 1 || verified != len(flatPayload) {
		t.Fatalf("windows-installer.xml signed installers=%d verified payload=%d", signedInstaller, verified)
	}
	if len(installer.Zip.FileSets) != 1 {
		t.Fatalf("windows-installer.xml pe-file-sets = %d, want 1", len(installer.Zip.FileSets))
	}
	installerSet := installer.Zip.FileSets[0]
	if installerSet.ForEach.Verify == nil || installerSet.ForEach.Sign != nil {
		t.Error("windows-installer.xml pe-file-set must verify, not re-sign, the app/ tree")
	}
	for _, want := range []string{"app/**/*.exe", "app/**/*.dll"} {
		found := false
		for _, include := range installerSet.Includes {
			if include.Path == want {
				found = true
			}
		}
		if !found {
			t.Errorf("windows-installer.xml pe-file-set must include %s", want)
		}
	}

	testInstaller := parseSignPathConfiguration(t, "windows-installer-test-v2.xml")
	if len(testInstaller.Zip.Files) != 1 {
		t.Fatalf("windows-installer-test-v2.xml files = %d, want 1", len(testInstaller.Zip.Files))
	}
	file := testInstaller.Zip.Files[0]
	if file.Path != "*installer*.exe" || file.Sign == nil || file.Verify != nil {
		t.Fatal("windows-installer-test-v2.xml must only sign the outer installer")
	}
}
