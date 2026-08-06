package main

import (
	"os"
	"strings"
	"testing"
)

func TestDesktopBuildLinksSharedSourceRevision(t *testing.T) {
	// The remote protocol source-revision ldflag was removed with the Remote
	// Workbench protocol stack; the test now asserts the shared product docs
	// identity and the packaging-order invariant below.
	data, err := os.ReadFile("../scripts/desktop-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, want := range []string{
		`SOURCE_REVISION="$(git -C "$ROOT" rev-parse --verify HEAD)"`,
		`SOURCE_REVISION="$SOURCE_REVISION+dirty"`,
		`product_docs_ldflags="-X reasonix/internal/productdocs.linkedVersion=$VERSION -X reasonix/internal/productdocs.linkedRevision=$SOURCE_REVISION"`,
		`cli_identity_ldflags="-X main.version=$VERSION -X main.gitCommit=$GIT_COMMIT -X main.buildTimeUTC=$BUILD_TIME_UTC $product_docs_ldflags"`,
		`ldflags="-X main.version=$VERSION -X main.channel=$CHANNEL $product_docs_ldflags"`,
		`cli_identity_ldflags`,
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
