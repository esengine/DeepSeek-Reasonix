package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestRepositoryReleaseSigningContract(t *testing.T) {
	root := "../.."
	contract, err := loadAndValidate(root)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := contractFingerprint(root, contract)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", fingerprint); len(got) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(got))
	}
}

func TestTopLevelSignPathWorkflowCallGraph(t *testing.T) {
	got, err := discoverTopLevelSigningWorkflows("../..")
	if err != nil {
		t.Fatal(err)
	}
	// The studio line signs from its release and its smoke test. Read from the
	// call graph, not the contract, so a third workflow that learns to reach the
	// signer fails here even if nobody widened the contract.
	want := []string{
		".github/workflows/release-studio.yml",
		".github/workflows/studio-certum-signing-smoke.yml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level workflows that reach SignPath = %v, want %v", got, want)
	}
}

func TestReleaseSigningContractRejectsWildcards(t *testing.T) {
	contract, err := loadAndValidate("../..")
	if err != nil {
		t.Fatal(err)
	}
	contract.AllowedBuildDefinitions = []string{".github/workflows/release-*.yml"}
	if err := validateContract("../..", contract); err == nil {
		t.Fatal("wildcard build definition unexpectedly passed validation")
	}
}

func TestWorkflowUsingSignPathTokenIsSigningEntryPoint(t *testing.T) {
	workflow := []byte(`
on: workflow_dispatch
jobs:
  sign:
    runs-on: windows-latest
    steps:
      - shell: pwsh
        env:
          SIGNPATH_API_TOKEN: ${{ secrets.SIGNPATH_API_TOKEN }}
        run: ./submit-signing-request.ps1
`)
	info, err := parseWorkflow(workflow)
	if err != nil {
		t.Fatal(err)
	}
	if !info.externallyTriggered || !info.directSigning {
		t.Fatalf("token-backed workflow was not classified as a signing entry point: %+v", info)
	}
}

func TestWorkflowUsingCertumCredentialsIsSigningEntryPoint(t *testing.T) {
	for _, secret := range []string{"CERTUM_OTP_URI"} {
		info, err := parseWorkflow([]byte(`
on: workflow_dispatch
jobs:
  sign:
    runs-on: windows-2022
    steps:
      - uses: ./.github/actions/setup-certum
        with:
          otp-uri: ${{ secrets.` + secret + ` }}
`))
		if err != nil {
			t.Fatal(err)
		}
		if !info.externallyTriggered || !info.directSigning {
			t.Fatalf("%s signing entry point not detected: %+v", secret, info)
		}
	}
}

func TestFingerprintMustPinTheScriptsItRuns(t *testing.T) {
	contract, err := loadAndValidate("../..")
	if err != nil {
		t.Fatal(err)
	}
	var without []string
	for _, name := range contract.FingerprintFiles {
		if name != "scripts/windows-signing-lib.ps1" {
			without = append(without, name)
		}
	}
	contract.FingerprintFiles = without
	if err := validateContract("../..", contract); err == nil {
		t.Fatal("a dot-sourced signing script left out of fingerprint_files passed validation")
	}
}
