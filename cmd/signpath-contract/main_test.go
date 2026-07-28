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
	want := []string{
		".github/workflows/release-desktop.yml",
		".github/workflows/release-stable.yml",
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
