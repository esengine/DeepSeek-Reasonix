package evidence

import (
	"strings"

	"reasonix/internal/base/fileutil"
)

// PathRole is what a changed path is to the work product. The zero value is
// production: a path no rule recognizes is code until something says otherwise,
// so an unfamiliar layout raises the risk it deserves instead of lowering it.
type PathRole uint8

const (
	// PathProduction is the work itself — what a reviewer has to look at.
	PathProduction PathRole = iota
	// PathSupporting ships with the work without being it: tests, fixtures,
	// documentation, localization, presentation.
	PathSupporting
	// PathVCSStore is the repository's own store, which is evidence of
	// nothing at any risk level (see fileutil.UnderVCSStore).
	PathVCSStore
)

// ClassifyPath is the single answer to what a changed path is, read by risk
// scoring and review coverage alike. Only a toolchain's own contract lowers a
// path from production: `/i18n/` held a number-formatting module, so reading a
// directory name as "not really code" waived the review of code. Lowering the
// bar is where a wrong guess costs something, so it gets no guesses.
func ClassifyPath(path string) PathRole {
	if strings.TrimSpace(path) == "" {
		return PathSupporting
	}
	if fileutil.UnderVCSStore(path) {
		return PathVCSStore
	}
	lower := strings.ToLower(fileutil.NormalizeSlashPath(path))
	switch {
	// Toolchain contracts, not conventions: `go build` excludes _test.go and
	// testdata outright, and the runners match these names to decide what is a
	// test. What the build refuses to ship is not what review was called for.
	case strings.HasSuffix(lower, "_test.go"), strings.Contains(lower, "/testdata/"):
		return PathSupporting
	case strings.HasSuffix(lower, ".test.ts"), strings.HasSuffix(lower, ".test.tsx"),
		strings.HasSuffix(lower, "_test.ts"), strings.HasSuffix(lower, ".spec.ts"),
		strings.HasSuffix(lower, "_spec.ts"), strings.Contains(lower, "/__tests__/"):
		return PathSupporting
	// Prose formats carry no behaviour to review, whatever directory they sit in.
	case strings.HasSuffix(lower, ".md"), strings.HasSuffix(lower, ".mdx"),
		strings.HasSuffix(lower, ".txt"), strings.HasSuffix(lower, ".rst"):
		return PathSupporting
	}
	return PathProduction
}
