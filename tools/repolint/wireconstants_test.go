package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestWireConstantParityRejectsEitherSideDrifting(t *testing.T) {
	root := testenv.TempDir(t)
	pair := []wireConstantMirror{{"code.go", "Code", "code.ts", "Code"}}
	for _, tc := range []struct {
		goSource, tsSource string
		want               int
	}{
		{`package p; const Code = "tree"`, `export const Code = "tree";`, 0},
		{`package p; const Code = "changed"`, `export const Code = "tree";`, 1},
		{`package p; const Code = "tree"`, `export const Code = "changed";`, 1},
		{`package p; const Renamed = "tree"`, `export const Code = "tree";`, 1},
		{`package p; const Code = "tree"`, `export const Renamed = "tree";`, 1},
	} {
		if err := os.WriteFile(filepath.Join(root, "code.go"), []byte(tc.goSource), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "code.ts"), []byte(tc.tsSource), 0600); err != nil {
			t.Fatal(err)
		}
		if got := wireConstantFindings(root, pair); len(got) != tc.want {
			t.Fatalf("%+v: %+v", tc, got)
		}
	}
}
