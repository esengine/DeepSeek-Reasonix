package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const generatedContractDir = "frontend/src/generated"

func TestHostContractGeneratedFilesAreCurrent(t *testing.T) {
	dir := t.TempDir()
	if err := emitContract(dir); err != nil {
		t.Fatalf("emit contract: %v", err)
	}
	for _, name := range []string{contractTSFile, contractJSONFile} {
		want, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(generatedContractDir, name))
		if err != nil {
			t.Fatalf("%v\nregenerate with: cd desktop && go run . -emit-contract %s", err, generatedContractDir)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s/%s is stale; regenerate with: cd desktop && go run . -emit-contract %s", generatedContractDir, name, generatedContractDir)
		}
	}
}

func TestHostContractFlagParsing(t *testing.T) {
	for _, tc := range []struct {
		args []string
		dir  string
		ok   bool
	}{
		{[]string{"-emit-contract", "out"}, "out", true},
		{[]string{"--emit-contract", "out"}, "out", true},
		{[]string{"-emit-contract=out/dir"}, "out/dir", true},
		{[]string{"-emit-contract"}, "", false},
		{[]string{"--host-rpc"}, "", false},
	} {
		dir, ok := emitContractDir(tc.args)
		if dir != tc.dir || ok != tc.ok {
			t.Errorf("emitContractDir(%v) = %q, %v; want %q, %v", tc.args, dir, ok, tc.dir, tc.ok)
		}
	}
	if !hostRPCRequested([]string{"launch", "--host-rpc"}) || hostRPCRequested([]string{"--safe-mode"}) {
		t.Fatal("hostRPCRequested must match the exact --host-rpc flag")
	}
}
