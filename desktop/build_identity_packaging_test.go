package main

import (
	"os"
	"strings"
	"testing"
)

func TestDesktopBuildLinksSharedSourceRevision(t *testing.T) {
	data, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		`SOURCE_REVISION="$(git -C "$ROOT" rev-parse --verify HEAD)"`,
		`SOURCE_REVISION="$SOURCE_REVISION+dirty"`,
		`source_revision_ldflag="-X reasonix/internal/remote/protocol.linkedSourceRevision=$SOURCE_REVISION"`,
		`ldflags="-X main.version=$VERSION -X main.channel=$CHANNEL $source_revision_ldflag"`,
		`-X main.version=$VERSION $source_revision_ldflag`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("desktop-build.sh does not preserve the shared build identity %q", want)
		}
	}

	revisionIndex := strings.Index(script, `SOURCE_REVISION="$(git -C "$ROOT" rev-parse --verify HEAD)"`)
	packagingMutationIndex := strings.Index(script, `node -e 'const fs=require("fs")`)
	if revisionIndex < 0 || packagingMutationIndex < 0 || revisionIndex >= packagingMutationIndex {
		t.Fatal("desktop-build.sh must capture the source revision before mutating packaging metadata")
	}
}
