package doublestar

import (
	"path/filepath"
	"testing"
)

var filepathGlobTests = []string{
	".",
	"././.",
	"..",
	"../.",
	".././././",
	"../..",
	"/",
	"./",
	"/.",
	"/././././",
	"nopermission/.",
}

func TestSpecialFilepathGlobCases(t *testing.T) {
	for idx, pattern := range filepathGlobTests {
		testSpecialFilepathGlobCasesWith(t, idx, pattern)
	}
}

func testSpecialFilepathGlobCasesWith(t *testing.T, idx int, pattern string) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. FilepathGlob(%#q) panicked with: %#v", idx, pattern, r)
		}
	}()

	pattern = filepath.FromSlash(pattern)
	matches, err := FilepathGlob(pattern)
	results, stdErr := filepath.Glob(pattern)

	// doublestar.FilepathGlob Cleans the path
	for idx, result := range results {
		results[idx] = filepath.Clean(result)
	}
	if !compareSlices(matches, results) || !compareErrors(err, stdErr) {
		t.Errorf("#%v. FilepathGlob(%#q) != filepath.Glob(%#q). Got %#v, %v want %#v, %v", idx, pattern, pattern, matches, err, results, stdErr)
	}
}
