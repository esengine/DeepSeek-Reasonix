package doublestar

import (
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type MatchTest struct {
	pattern, testPath     string // a pattern and path to test the pattern on
	shouldMatch           bool   // true if the pattern should match the path
	shouldMatchGlob       bool   // true if glob should match the path
	caseSensitive         bool   // match is case sensitive
	expectedErr           error  // an expected error
	expectIOErr           bool   // whether or not to expect an io error
	expectPatternNotExist bool   // whether or not to expect ErrPatternNotExist
	isStandard            bool   // pattern doesn't use any doublestar features (e.g. '**', '{a,b}')
	testOnDisk            bool   // true: test pattern against files in "testdata" directory
	numResults            int    // number of glob results if testing on disk
	winNumResults         int    // number of glob results on Windows
}

// Tests which contain escapes and symlinks will not work on Windows
var onWindows = runtime.GOOS == "windows"

var matchTests = []MatchTest{
	{"", "", true, false, false, nil, true, false, true, true, 0, 0},
	{"*", "", true, true, false, nil, false, false, true, false, 0, 0},
	{"*", "/", false, false, false, nil, false, false, true, false, 0, 0},
	{"/*", "/", true, true, false, nil, false, false, true, false, 0, 0},
	{"/*", "/debug/", false, false, false, nil, false, false, true, false, 0, 0},
	{"/*", "//", false, false, false, nil, false, false, true, false, 0, 0},
	{"abc", "abc", true, true, false, nil, false, false, true, true, 1, 1},
	{"*", "abc", true, true, false, nil, false, false, true, true, 26, 21},
	{"*c", "abc", true, true, false, nil, false, false, true, true, 2, 2},
	{"*/", "a/", true, true, false, nil, false, false, true, false, 0, 0},
	{"a*", "a", true, true, false, nil, false, false, true, true, 9, 9},
	{"a*", "abc", true, true, false, nil, false, false, true, true, 9, 9},
	{"a*", "ab/c", false, false, false, nil, false, false, true, true, 9, 9},
	{"a*/b", "abc/b", true, true, false, nil, false, false, true, true, 2, 2},
	{"a*/b", "a/c/b", false, false, false, nil, false, false, true, true, 2, 2},
	{"a*/c/", "a/b", false, false, false, nil, false, false, false, true, 1, 1},
	{"a*b*c*d*e*", "axbxcxdxe", true, true, false, nil, false, false, true, true, 3, 3},
	{"a*b*c*d*e*/f", "axbxcxdxe/f", true, true, false, nil, false, false, true, true, 2, 2},
	{"a*b*c*d*e*/f", "axbxcxdxexxx/f", true, true, false, nil, false, false, true, true, 2, 2},
	{"a*b*c*d*e*/f", "axbxcxdxe/xxx/f", false, false, false, nil, false, false, true, true, 2, 2},
	{"a*b*c*d*e*/f", "axbxcxdxexxx/fff", false, false, false, nil, false, false, true, true, 2, 2},
	{"a*b?c*x", "abxbbxdbxebxczzx", true, true, false, nil, false, false, true, true, 2, 2},
	{"a*b?c*x", "abxbbxdbxebxczzy", false, false, false, nil, false, false, true, true, 2, 2},
	{"ab[c]", "abc", true, true, false, nil, false, false, true, true, 1, 1},
	{"ab[b-d]", "abc", true, true, false, nil, false, false, true, true, 1, 1},
	{"ab[e-g]", "abc", false, false, false, nil, false, false, true, true, 0, 0},
	{"ab[^c]", "abc", false, false, false, nil, false, false, true, true, 0, 0},
	{"ab[^b-d]", "abc", false, false, false, nil, false, false, true, true, 0, 0},
	{"ab[^e-g]", "abc", true, true, false, nil, false, false, true, true, 1, 1},
	{"a\\*b", "ab", false, false, false, nil, false, true, true, !onWindows, 0, 0},
	{"a?b", "a☺b", true, true, false, nil, false, false, true, true, 1, 1},
	{"a[^a]b", "a☺b", true, true, false, nil, false, false, true, true, 1, 1},
	{"a[!a]b", "a☺b", true, true, false, nil, false, false, false, true, 1, 1},
	{"a???b", "a☺b", false, false, false, nil, false, false, true, true, 0, 0},
	{"a[^a][^a][^a]b", "a☺b", false, false, false, nil, false, false, true, true, 0, 0},
	{"[a-ζ]*", "α", true, true, false, nil, false, false, true, true, 23, 20},
	{"*[a-ζ]", "A", false, false, false, nil, false, false, true, true, 23, 20},
	{"a?b", "a/b", false, false, false, nil, false, false, true, true, 1, 1},
	{"a*b", "a/b", false, false, false, nil, false, false, true, true, 1, 1},
	{"[\\]a]", "]", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"[\\-]", "-", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"[x\\-]", "x", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"[x\\-]", "-", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"[x\\-]", "z", false, false, false, nil, false, false, true, !onWindows, 2, 2},
	{"[\\-x]", "x", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"[\\-x]", "-", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"[\\-x]", "a", false, false, false, nil, false, false, true, !onWindows, 2, 2},
	{"[]a]", "]", false, false, false, ErrBadPattern, false, false, true, true, 0, 0},
	// doublestar, like bash, allows these when path.Match() does not
	{"[-]", "-", true, true, false, nil, false, false, false, !onWindows, 1, 0},
	{"[x-]", "x", true, true, false, nil, false, false, false, true, 2, 1},
	{"[x-]", "-", true, true, false, nil, false, false, false, !onWindows, 2, 1},
	{"[x-]", "z", false, false, false, nil, false, false, false, true, 2, 1},
	{"[-x]", "x", true, true, false, nil, false, false, false, true, 2, 1},
	{"[-x]", "-", true, true, false, nil, false, false, false, !onWindows, 2, 1},
	{"[-x]", "a", false, false, false, nil, false, false, false, true, 2, 1},
	{"[a-b-d]", "a", true, true, false, nil, false, false, false, true, 3, 2},
	{"[a-b-d]", "b", true, true, false, nil, false, false, false, true, 3, 2},
	{"[a-b-d]", "-", true, true, false, nil, false, false, false, !onWindows, 3, 2},
	{"[a-b-d]", "c", false, false, false, nil, false, false, false, true, 3, 2},
	{"[a-b-x]", "x", true, true, false, nil, false, false, false, true, 4, 3},
	{"\\", "a", false, false, false, ErrBadPattern, false, false, true, !onWindows, 0, 0},
	{"[", "a", false, false, false, ErrBadPattern, false, false, true, true, 0, 0},
	{"[^", "a", false, false, false, ErrBadPattern, false, false, true, true, 0, 0},
	{"[^bc", "a", false, false, false, ErrBadPattern, false, false, true, true, 0, 0},
	{"a[", "a", false, false, false, ErrBadPattern, false, false, true, true, 0, 0},
	{"a[", "ab", false, false, false, ErrBadPattern, false, false, true, true, 0, 0},
	{"ad[", "ab", false, false, false, ErrBadPattern, false, false, true, true, 0, 0},
	{"*x", "xxx", true, true, false, nil, false, false, true, true, 4, 4},
	{"[abc]", "b", true, true, false, nil, false, false, true, true, 3, 3},
	{"[abc123]", "1", true, true, false, nil, false, false, true, true, 4, 4},
	{"[a-z0-9]", "1", true, true, false, nil, false, false, true, true, 8, 8},
	{"**", "", true, true, false, nil, false, false, false, false, 38, 38},
	{"a/**", "a", true, true, false, nil, false, false, false, true, 7, 7},
	{"a/**/", "a", true, true, false, nil, false, false, false, true, 4, 4},
	{"a/**", "a/", true, true, false, nil, false, false, false, false, 7, 7},
	{"a/**/", "a/", true, true, false, nil, false, false, false, false, 4, 4},
	{"a/**", "a/b", true, true, false, nil, false, false, false, true, 7, 7},
	{"a/**", "a/b/c", true, true, false, nil, false, false, false, true, 7, 7},
	{"**/c", "c", true, true, false, nil, !onWindows, false, false, true, 6, 5},
	{"**/c", "b/c", true, true, false, nil, !onWindows, false, false, true, 6, 5},
	{"**/c", "a/b/c", true, true, false, nil, !onWindows, false, false, true, 6, 5},
	{"**/c", "a/b", false, false, false, nil, !onWindows, false, false, true, 6, 5},
	{"**/c", "abcd", false, false, false, nil, !onWindows, false, false, true, 6, 5},
	{"**/c", "a/abc", false, false, false, nil, !onWindows, false, false, true, 6, 5},
	{"a/**/b", "a/b", true, true, false, nil, false, false, false, true, 2, 2},
	{"a/**/c", "a/b/c", true, true, false, nil, false, false, false, true, 2, 2},
	{"a/**/d", "a/b/c/d", true, true, false, nil, false, false, false, true, 1, 1},
	{"a/\\**", "a/b/c", false, false, false, nil, false, false, false, !onWindows, 0, 0},
	{"a/\\[*\\]", "a/bc", false, false, false, nil, false, false, true, !onWindows, 0, 0},
	// this fails the FilepathGlob test on Windows
	{"a/b/c", "a/b//c", false, false, false, nil, false, false, true, !onWindows, 1, 1},
	// odd: Glob + filepath.Glob return results
	{"a/", "a", false, false, false, nil, false, false, true, false, 0, 0},
	{"ab{c,d}", "abc", true, true, false, nil, false, true, false, true, 1, 1},
	{"ab{c,d,*}", "abcde", true, true, false, nil, false, true, false, true, 5, 5},
	{"ab{c,d}[", "abcd", false, false, false, ErrBadPattern, false, false, false, true, 0, 0},
	{"a{,bc}", "a", true, true, false, nil, false, false, false, true, 2, 2},
	{"a{,bc}", "abc", true, true, false, nil, false, false, false, true, 2, 2},
	{"a/{b/c,c/b}", "a/b/c", true, true, false, nil, false, false, false, true, 2, 2},
	{"a/{b/c,c/b}", "a/c/b", true, true, false, nil, false, false, false, true, 2, 2},
	{"a/a*{b,c}", "a/abc", true, true, false, nil, false, false, false, true, 1, 1},
	{"{a/{b,c},abc}", "a/b", true, true, false, nil, false, false, false, true, 3, 3},
	{"{a/{b,c},abc}", "a/c", true, true, false, nil, false, false, false, true, 3, 3},
	{"{a/{b,c},abc}", "abc", true, true, false, nil, false, false, false, true, 3, 3},
	{"{a/{b,c},abc}", "a/b/c", false, false, false, nil, false, false, false, true, 3, 3},
	{"{a/ab*}", "a/abc", true, true, false, nil, false, false, false, true, 1, 1},
	{"{a/*}", "a/b", true, true, false, nil, false, false, false, true, 3, 3},
	{"{a/abc}", "a/abc", true, true, false, nil, false, false, false, true, 1, 1},
	{"{a/b,a/c}", "a/c", true, true, false, nil, false, false, false, true, 2, 2},
	{"abc/**", "abc/b", true, true, false, nil, false, false, false, true, 3, 3},
	{"**/abc", "abc", true, true, false, nil, !onWindows, false, false, true, 2, 2},
	{"abc**", "abc/b", false, false, false, nil, false, false, false, true, 3, 3},
	{"**/*.txt", "abc/【test】.txt", true, true, false, nil, !onWindows, false, false, true, 1, 1},
	{"**/【*", "abc/【test】.txt", true, true, false, nil, !onWindows, false, false, true, 1, 1},
	{"**/{a,b}", "a/b", true, true, false, nil, !onWindows, false, false, true, 7, 5},
	{"a/*/*/d", "a/b/c/d", true, true, false, nil, false, false, true, true, 1, 1},
	// unfortunately, io/fs can't handle this, so neither can Glob =(
	{"broken-symlink", "broken-symlink", true, true, false, nil, false, false, true, false, 1, 1},
	{"broken-symlink/*", "a", false, false, false, nil, false, true, true, true, 0, 0},
	{"broken*/*", "a", false, false, false, nil, false, false, true, true, 0, 0},
	{"working-symlink/c/*", "working-symlink/c/d", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"working-sym*/*", "working-symlink/c", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"b/**/f", "b/symlink-dir/f", true, true, false, nil, false, false, false, !onWindows, 2, 2},
	{"*/symlink-dir/*", "b/symlink-dir/f", true, true, false, nil, !onWindows, false, true, !onWindows, 2, 2},
	{"e/\\[x\\]/*", "e/[x]/[y]", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/\\[x\\]/*/z", "e/[x]/[y]/z", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/**/{z,other}", "e/[x]/[y]/z", true, true, false, nil, false, false, false, !onWindows, 1, 0},
	{"f/\\{a,b\\}/{c,other*}", "f/{a,b}/c", true, true, false, nil, false, false, false, !onWindows, 1, 0},
	{"f/\\*/{a,other*}", "f/*/a", true, true, false, nil, false, false, false, !onWindows, 1, 0},
	{"f/\\?/{a,other*}", "f/?/a", true, true, false, nil, false, false, false, !onWindows, 1, 0},
	{"e/**", "e/**", true, true, false, nil, false, false, false, !onWindows, 14, 9},
	{"e/**", "e/*", true, true, false, nil, false, false, false, !onWindows, 14, 9},
	{"e/**", "e/?", true, true, false, nil, false, false, false, !onWindows, 14, 9},
	{"e/**", "e/[", true, true, false, nil, false, false, false, true, 14, 9},
	{"e/**", "e/]", true, true, false, nil, false, false, false, true, 14, 9},
	{"e/**", "e/[]", true, true, false, nil, false, false, false, true, 14, 9},
	{"e/**", "e/{", true, true, false, nil, false, false, false, true, 14, 9},
	{"e/**", "e/}", true, true, false, nil, false, false, false, true, 14, 9},
	{"e/**", "e/\\", true, true, false, nil, false, false, false, !onWindows, 14, 6},
	{"e/*", "e/*", true, true, false, nil, false, false, true, !onWindows, 11, 5},
	{"e/?", "e/?", true, true, false, nil, false, false, true, !onWindows, 7, 4},
	{"e/?", "e/*", true, true, false, nil, false, false, true, !onWindows, 7, 4},
	{"e/?", "e/[", true, true, false, nil, false, false, true, true, 7, 4},
	{"e/?", "e/]", true, true, false, nil, false, false, true, true, 7, 4},
	{"e/?", "e/{", true, true, false, nil, false, false, true, true, 7, 4},
	{"e/?", "e/}", true, true, false, nil, false, false, true, true, 7, 4},
	{"e/\\[", "e/[", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/[", "e/[", false, false, false, ErrBadPattern, false, false, true, true, 0, 0},
	{"e/]", "e/]", true, true, false, nil, false, false, true, true, 1, 1},
	{"e/\\]", "e/]", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/\\{", "e/{", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/\\}", "e/}", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/[\\*\\?]", "e/*", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"e/[\\*\\?]", "e/?", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"e/[\\*\\?]", "e/**", false, false, false, nil, false, false, true, !onWindows, 2, 2},
	{"e/[\\*\\?]?", "e/**", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/{\\*,\\?}", "e/*", true, true, false, nil, false, false, false, !onWindows, 2, 2},
	{"e/{\\*,\\?}", "e/?", true, true, false, nil, false, false, false, !onWindows, 2, 2},
	{"e/\\*", "e/*", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/\\?", "e/?", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"e/\\?", "e/**", false, false, false, nil, false, false, true, !onWindows, 1, 1},
	{"*\\}", "}", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"case-sensitive/fi*", "case-sensitive/file", true, true, false, nil, false, false, true, !onWindows, 1, 1},
	{"case-sensitive/FI*", "case-sensitive/FILE", true, true, true, nil, false, false, true, !onWindows, 1, 1},
	{"case-sensitive/Fi*", "case-sensitive/File", true, true, true, nil, false, false, true, !onWindows, 1, 1},
	{"nonexistent-path", "a", false, false, false, nil, false, true, true, true, 0, 0},
	{"nonexistent-path/", "a", false, false, false, nil, false, true, true, true, 0, 0},
	{"nonexistent-path/file", "a", false, false, false, nil, false, true, true, true, 0, 0},
	{"nonexistent-path/*", "a", false, false, false, nil, false, true, true, true, 0, 0},
	{"nonexistent-path/**", "a", false, false, false, nil, false, true, true, true, 0, 0},
	{"nopermission/*", "nopermission/file", true, false, false, nil, true, false, true, !onWindows, 0, 0},
	{"nopermission/dir/", "nopermission/dir", false, false, false, nil, true, false, true, !onWindows, 0, 0},
	{"nopermission/file", "nopermission/file", true, false, false, nil, true, false, true, !onWindows, 0, 0},
	{"hidden-tests/.*", "hidden-tests/.hidden-file", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"hidden-tests/?hidden*", "hidden-tests/.hidden-file", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"hidden-tests/*", "hidden-tests/visible-file", true, true, false, nil, false, false, true, true, 3, 3},
	{"hidden-tests/**", "hidden-tests/.hidden-dir/.hidden-file", true, true, false, nil, false, false, false, !onWindows, 6, 6},
	{"hidden-tests/**", "hidden-tests/hidden-dir/hidden-file", true, true, false, nil, false, false, false, onWindows, 6, 6},
	{"hidden-tests/.hidden-dir/*", "hidden-tests/.hidden-dir/.hidden-file", true, true, false, nil, false, false, true, !onWindows, 2, 2},
	{"hidden-tests/hidden-dir/*", "hidden-tests/hidden-dir/hidden-file", true, true, false, nil, false, false, true, onWindows, 2, 2},
}

// True if the file system supports case-sensitive filenames
var fsIsCaseSensitive = false

// Calculate the number of results that we expect
// WithFilesOnly at runtime and memoize them here
var numResultsFilesOnly []int

// Calculate the number of results that we expect
// WithNoFollow at runtime and memoize them here
var numResultsNoFollow []int

// Calculate the number of results that we expect
// WithNoHidden at runtime and memoize them here
var numResultsNoHidden []int

// Calculate the number of results that we expect with all
// of the options enabled at runtime and memoize them here
var numResultsAllOpts []int

func TestValidatePattern(t *testing.T) {
	for idx, tt := range matchTests {
		testValidatePatternWith(t, idx, tt)
	}
}

func testValidatePatternWith(t *testing.T, idx int, tt MatchTest) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. Validate(%#q) panicked: %#v", idx, tt.pattern, r)
		}
	}()

	result := ValidatePattern(tt.pattern)
	if result != (tt.expectedErr == nil) {
		t.Errorf("#%v. ValidatePattern(%#q) = %v want %v", idx, tt.pattern, result, !result)
	}
}

func TestMatch(t *testing.T) {
	for idx, tt := range matchTests {
		// Since Match() always uses "/" as the separator, we
		// don't need to worry about the tt.testOnDisk flag
		testMatchWith(t, idx, tt)
	}
}

func testMatchWith(t *testing.T, idx int, tt MatchTest) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. Match(%#q, %#q) panicked: %#v", idx, tt.pattern, tt.testPath, r)
		}
	}()

	// Match() always uses "/" as the separator
	ok, err := Match(tt.pattern, tt.testPath)
	if ok != tt.shouldMatch || err != tt.expectedErr {
		t.Errorf("#%v. Match(%#q, %#q) = %v, %v want %v, %v", idx, tt.pattern, tt.testPath, ok, err, tt.shouldMatch, tt.expectedErr)
	}

	if tt.isStandard {
		stdOk, stdErr := path.Match(tt.pattern, tt.testPath)
		if ok != stdOk || !compareErrors(err, stdErr) {
			t.Errorf("#%v. Match(%#q, %#q) != path.Match(...). Got %v, %v want %v, %v", idx, tt.pattern, tt.testPath, ok, err, stdOk, stdErr)
		}
	}
}

func BenchmarkMatch(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, tt := range matchTests {
			if tt.isStandard {
				Match(tt.pattern, tt.testPath)
			}
		}
	}
}

func BenchmarkGoMatch(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, tt := range matchTests {
			if tt.isStandard {
				path.Match(tt.pattern, tt.testPath)
			}
		}
	}
}

func TestMatchUnvalidated(t *testing.T) {
	for idx, tt := range matchTests {
		testMatchUnvalidatedWith(t, idx, tt)
	}

	// Not only test that they get the right matches, but make sure Unvalidated is properly *skipping* validation
	unvalidatedTests := []struct {
		pattern, testPath      string // a pattern and path to test the pattern on
		expectedErrValidated   error  // the error expected if the pattern was being validated
		expectedErrUnvalidated error  // the error expected if the pattern was not being validated
	}{
		{"", "", nil, nil},
		{"*", "", nil, nil},
		{"a[", "a", ErrBadPattern, nil},          // End early because got to end of match; do not need to validate the rest
		{"a[", "b", ErrBadPattern, nil},          // End early because failed to match; do not need to validate the rest
		{"[", "a", ErrBadPattern, ErrBadPattern}, // Error right up front, needs to fail whether validate or not
	}
	for idx, tt := range unvalidatedTests {
		_, errValidated := matchWithSeparator(tt.pattern, tt.testPath, '/', true, false)
		_, errUnvalidated := matchWithSeparator(tt.pattern, tt.testPath, '/', false, false)
		if errValidated != tt.expectedErrValidated {
			t.Errorf("#%v. Validated error of Match(%#q, %#q) = %v want %v", idx, tt.pattern, tt.testPath, errValidated, tt.expectedErrValidated)
		}
		if errUnvalidated != tt.expectedErrUnvalidated {
			t.Errorf("#%v. Unvalidated error of Match(%#q, %#q) = %v want %v", idx, tt.pattern, tt.testPath, errUnvalidated, tt.expectedErrUnvalidated)
		}
	}
}

func testMatchUnvalidatedWith(t *testing.T, idx int, tt MatchTest) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. MatchUnvalidated(%#q, %#q) panicked: %#v", idx, tt.pattern, tt.testPath, r)
		}
	}()

	// MatchUnvalidated() always uses "/" as the separator
	ok := MatchUnvalidated(tt.pattern, tt.testPath)
	if ok != tt.shouldMatch {
		t.Errorf("#%v. MatchUnvalidated(%#q, %#q) = %v want %v", idx, tt.pattern, tt.testPath, ok, tt.shouldMatch)
	}

	if tt.isStandard {
		stdOk, _ := path.Match(tt.pattern, tt.testPath)
		if ok != stdOk {
			t.Errorf("#%v. MatchUnvalidated(%#q, %#q) != path.Match(...). Got %v want %v", idx, tt.pattern, tt.testPath, ok, stdOk)
		}
	}
}

func TestPathMatch(t *testing.T) {
	for idx, tt := range matchTests {
		// Even though we aren't actually matching paths on disk, we are using
		// PathMatch() which will use the system's separator. As a result, any
		// patterns that might cause problems on-disk need to also be avoided
		// here in this test.
		if tt.testOnDisk {
			testPathMatchWith(t, idx, tt)
		}
	}
}

func testPathMatchWith(t *testing.T, idx int, tt MatchTest) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. Match(%#q, %#q) panicked: %#v", idx, tt.pattern, tt.testPath, r)
		}
	}()

	pattern := filepath.FromSlash(tt.pattern)
	testPath := filepath.FromSlash(tt.testPath)
	ok, err := PathMatch(pattern, testPath)
	if ok != tt.shouldMatch || err != tt.expectedErr {
		t.Errorf("#%v. PathMatch(%#q, %#q) = %v, %v want %v, %v", idx, pattern, testPath, ok, err, tt.shouldMatch, tt.expectedErr)
	}

	if tt.isStandard {
		stdOk, stdErr := filepath.Match(pattern, testPath)
		if ok != stdOk || !compareErrors(err, stdErr) {
			t.Errorf("#%v. PathMatch(%#q, %#q) != filepath.Match(...). Got %v, %v want %v, %v", idx, pattern, testPath, ok, err, stdOk, stdErr)
		}
	}
}

func TestPathMatchFake(t *testing.T) {
	// This test fakes that our path separator is `\\` so we can test what it
	// would be like on Windows - obviously, we don't need to do that if we
	// actually _are_ on Windows, since TestPathMatch will cover it.
	if onWindows {
		return
	}

	for idx, tt := range matchTests {
		// Even though we aren't actually matching paths on disk, we are using
		// PathMatch() which will use the system's separator. As a result, any
		// patterns that might cause problems on-disk need to also be avoided
		// here in this test.
		// On Windows, escaping is disabled. Instead, '\\' is treated as path separator.
		// So it's not possible to match escaped wild characters.
		if tt.testOnDisk && !strings.Contains(tt.pattern, "\\") {
			testPathMatchFakeWith(t, idx, tt)
		}
	}
}

func testPathMatchFakeWith(t *testing.T, idx int, tt MatchTest) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. Match(%#q, %#q) panicked: %#v", idx, tt.pattern, tt.testPath, r)
		}
	}()

	pattern := strings.ReplaceAll(tt.pattern, "/", "\\")
	testPath := strings.ReplaceAll(tt.testPath, "/", "\\")
	ok, err := matchWithSeparator(pattern, testPath, '\\', true, false)
	if ok != tt.shouldMatch || err != tt.expectedErr {
		t.Errorf("#%v. PathMatch(%#q, %#q) = %v, %v want %v, %v", idx, pattern, testPath, ok, err, tt.shouldMatch, tt.expectedErr)
	}
}

func BenchmarkPathMatch(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, tt := range matchTests {
			if tt.isStandard && tt.testOnDisk {
				pattern := filepath.FromSlash(tt.pattern)
				testPath := filepath.FromSlash(tt.testPath)
				PathMatch(pattern, testPath)
			}
		}
	}
}

func BenchmarkGoPathMatch(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, tt := range matchTests {
			if tt.isStandard && tt.testOnDisk {
				pattern := filepath.FromSlash(tt.pattern)
				testPath := filepath.FromSlash(tt.testPath)
				filepath.Match(pattern, testPath)
			}
		}
	}
}

func TestGlob(t *testing.T) {
	doGlobTest(t, nil)
}

func TestGlobWithCaseInsensitive(t *testing.T) {
	if fsIsCaseSensitive {
		doGlobTest(t, nil, WithCaseInsensitive())
	}
}

func TestGlobWithFailOnIOErrors(t *testing.T) {
	doGlobTest(t, nil, WithFailOnIOErrors())
}

func TestGlobWithFailOnPatternNotExist(t *testing.T) {
	doGlobTest(t, nil, WithFailOnPatternNotExist())
}

func TestGlobWithFilesOnly(t *testing.T) {
	doGlobTest(t, numResultsFilesOnly, WithFilesOnly())
}

func TestGlobWithNoFollow(t *testing.T) {
	doGlobTest(t, numResultsNoFollow, WithNoFollow())
}

func TestGlobWithNoHidden(t *testing.T) {
	doGlobTest(t, numResultsNoHidden, WithNoHidden())
}

func TestGlobWithAllOptions(t *testing.T) {
	doGlobTest(t, numResultsAllOpts, WithCaseInsensitive(), WithFailOnIOErrors(), WithFailOnPatternNotExist(), WithFilesOnly(), WithNoFollow(), WithNoHidden())
}

func doGlobTest(t *testing.T, numResults []int, opts ...GlobOption) {
	glob := newGlob(opts...)
	fsys := os.DirFS("testdata")
	for idx, tt := range matchTests {
		if tt.testOnDisk && (!tt.caseSensitive || fsIsCaseSensitive) {
			expectedNum := tt.numResults
			if numResults != nil {
				expectedNum = numResults[idx]
			} else if onWindows {
				expectedNum = tt.winNumResults
			}
			testGlobWith(t, idx, tt, glob, opts, expectedNum, fsys)
		}
	}
}

func testGlobWith(t *testing.T, idx int, tt MatchTest, g *glob, opts []GlobOption, expectedNum int, fsys fs.FS) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. Glob(%#q, %#v) panicked: %#v", idx, tt.pattern, g, r)
		}
	}()

	matches, err := Glob(fsys, tt.pattern, opts...)
	verifyGlobResults(t, idx, "Glob", tt, g, matches, expectedNum, err)
	if len(opts) == 0 {
		testStandardGlob(t, idx, "Glob", tt, fsys, matches, err)
	}
}

func TestGlobWalk(t *testing.T) {
	doGlobWalkTest(t, nil)
}

func TestGlobWalkWithFailOnIOErrors(t *testing.T) {
	doGlobWalkTest(t, nil, WithFailOnIOErrors())
}

func TestGlobWalkWithFailOnPatternNotExist(t *testing.T) {
	doGlobWalkTest(t, nil, WithFailOnPatternNotExist())
}

func TestGlobWalkWithFilesOnly(t *testing.T) {
	doGlobWalkTest(t, numResultsFilesOnly, WithFilesOnly())
}

func TestGlobWalkWithNoFollow(t *testing.T) {
	doGlobWalkTest(t, numResultsNoFollow, WithNoFollow())
}

func TestGlobWalkWithNoHidden(t *testing.T) {
	doGlobWalkTest(t, numResultsNoHidden, WithNoHidden())
}

func TestGlobWalkWithAllOptions(t *testing.T) {
	doGlobWalkTest(t, numResultsAllOpts, WithFailOnIOErrors(), WithFailOnPatternNotExist(), WithFilesOnly(), WithNoFollow(), WithNoHidden())
}

func doGlobWalkTest(t *testing.T, numResults []int, opts ...GlobOption) {
	glob := newGlob(opts...)
	fsys := os.DirFS("testdata")
	for idx, tt := range matchTests {
		if tt.testOnDisk && (!tt.caseSensitive || fsIsCaseSensitive) {
			expectedNum := tt.numResults
			if numResults != nil {
				expectedNum = numResults[idx]
			} else if onWindows {
				expectedNum = tt.winNumResults
			}
			testGlobWalkWith(t, idx, tt, glob, opts, expectedNum, fsys)
		}
	}
}

func testGlobWalkWith(t *testing.T, idx int, tt MatchTest, g *glob, opts []GlobOption, expectedNum int, fsys fs.FS) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. Glob(%#q, %#v) panicked: %#v", idx, tt.pattern, opts, r)
		}
	}()

	var matches []string
	err := GlobWalk(fsys, tt.pattern, func(p string, d fs.DirEntry) error {
		matches = append(matches, p)
		return nil
	}, opts...)
	verifyGlobResults(t, idx, "GlobWalk", tt, g, matches, expectedNum, err)
	if len(opts) == 0 {
		testStandardGlob(t, idx, "GlobWalk", tt, fsys, matches, err)
	}
}

func testStandardGlob(t *testing.T, idx int, fn string, tt MatchTest, fsys fs.FS, matches []string, err error) {
	if tt.isStandard {
		stdMatches, stdErr := fs.Glob(fsys, tt.pattern)
		if !compareSlices(matches, stdMatches) || !compareErrors(err, stdErr) {
			t.Errorf("#%v. %v(%#q) != fs.Glob(...). Got %#v, %v want %#v, %v", idx, fn, tt.pattern, matches, err, stdMatches, stdErr)
		}
	}
}

func TestFilepathGlob(t *testing.T) {
	doFilepathGlobTest(t, nil)
}

func TestFilepathGlobWithFailOnIOErrors(t *testing.T) {
	doFilepathGlobTest(t, nil, WithFailOnIOErrors())
}

func TestFilepathGlobWithFailOnPatternNotExist(t *testing.T) {
	doFilepathGlobTest(t, nil, WithFailOnPatternNotExist())
}

func TestFilepathGlobWithFilesOnly(t *testing.T) {
	doFilepathGlobTest(t, numResultsFilesOnly, WithFilesOnly())
}

func TestFilepathGlobWithNoFollow(t *testing.T) {
	doFilepathGlobTest(t, numResultsNoFollow, WithNoFollow())
}

func TestFilepathGlobWithNoHidden(t *testing.T) {
	doFilepathGlobTest(t, numResultsNoHidden, WithNoHidden())
}

func doFilepathGlobTest(t *testing.T, numResults []int, opts ...GlobOption) {
	glob := newGlob(opts...)
	fsys := os.DirFS("testdata")

	// The patterns are relative to the "testdata" sub-directory.
	defer func() {
		os.Chdir("..")
	}()
	os.Chdir("testdata")

	for idx, tt := range matchTests {
		// Patterns ending with a slash are treated semantically different by
		// FilepathGlob vs Glob because FilepathGlob runs filepath.Clean, which
		// will remove the trailing slash.
		if tt.testOnDisk && (!tt.caseSensitive || fsIsCaseSensitive) && !strings.HasSuffix(tt.pattern, "/") {
			ttmod := tt
			ttmod.pattern = filepath.FromSlash(tt.pattern)
			ttmod.testPath = filepath.FromSlash(tt.testPath)

			expectedNum := tt.numResults
			if numResults != nil {
				expectedNum = numResults[idx]
			} else if onWindows {
				expectedNum = tt.winNumResults
			}
			testFilepathGlobWith(t, idx, ttmod, glob, opts, expectedNum, fsys)
		}
	}
}

func testFilepathGlobWith(t *testing.T, idx int, tt MatchTest, g *glob, opts []GlobOption, expectedNum int, fsys fs.FS) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("#%v. FilepathGlob(%#q, %#v) panicked: %#v", idx, tt.pattern, g, r)
		}
	}()

	matches, err := FilepathGlob(tt.pattern, opts...)
	verifyGlobResults(t, idx, "FilepathGlob", tt, g, matches, expectedNum, err)

	if tt.isStandard && len(opts) == 0 {
		stdMatches, stdErr := filepath.Glob(tt.pattern)
		if !compareSlices(matches, stdMatches) || !compareErrors(err, stdErr) {
			t.Errorf("#%v. FilepathGlob(%#q, %#v) != filepath.Glob(...). Got %#v, %v want %#v, %v", idx, tt.pattern, g, matches, err, stdMatches, stdErr)
		}
	}
}

func verifyGlobResults(t *testing.T, idx int, fn string, tt MatchTest, g *glob, matches []string, expectedNum int, err error) {
	expectedErr := tt.expectedErr
	if g.failOnPatternNotExist && tt.expectPatternNotExist {
		expectedErr = ErrPatternNotExist
	}

	if g.failOnIOErrors {
		if tt.expectIOErr {
			if err == nil {
				t.Errorf("#%v. %v(%#q, %#v) does not have an error, but should", idx, fn, tt.pattern, g)
			}
			return
		} else if err != nil && err != expectedErr {
			t.Errorf("#%v. %v(%#q, %#v) has error %v, but should not", idx, fn, tt.pattern, g, err)
			return
		}
	}

	if !g.failOnPatternNotExist || !tt.expectPatternNotExist {
		if strings.HasPrefix(tt.pattern, "case-sensitive") && g.caseInsensitive && fsIsCaseSensitive {
			expectedNum = 3
		}

		if len(matches) != expectedNum {
			t.Errorf("#%v. %v(%#q, %#v) = %#v - should have %#v results, got %#v", idx, fn, tt.pattern, g, matches, expectedNum, len(matches))
		}
		// Skip testPath check for noHidden since the match semantics are different
		if !g.filesOnly && !g.noFollow && !g.noHidden && inSlice(tt.testPath, matches) != tt.shouldMatchGlob {
			if tt.shouldMatchGlob {
				t.Errorf("#%v. %v(%#q, %#v) = %#v - doesn't contain %v, but should", idx, fn, tt.pattern, g, matches, tt.testPath)
			} else {
				t.Errorf("#%v. %v(%#q, %#v) = %#v - contains %v, but shouldn't", idx, fn, tt.pattern, g, matches, tt.testPath)
			}
		}
	}
	if err != expectedErr {
		t.Errorf("#%v. %v(%#q, %#v) has error %v, but should be %v", idx, fn, tt.pattern, g, err, expectedErr)
	}
}

func TestGlobSorted(t *testing.T) {
	fsys := os.DirFS("testdata")
	expected := []string{"a", "abc", "abcd", "abcde", "abxbbxdbxebxczzx", "abxbbxdbxebxczzy", "axbxcxdxe", "axbxcxdxexxx", "a☺b"}
	matches, err := Glob(fsys, "a*")
	if err != nil {
		t.Errorf("Unexpected error %v", err)
		return
	}

	if len(matches) != len(expected) {
		t.Errorf("Glob returned %#v; expected %#v", matches, expected)
		return
	}
	for idx, match := range matches {
		if match != expected[idx] {
			t.Errorf("Glob returned %#v; expected %#v", matches, expected)
			return
		}
	}
}

func BenchmarkGlob(b *testing.B) {
	fsys := os.DirFS("testdata")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, tt := range matchTests {
			if tt.isStandard && tt.testOnDisk {
				Glob(fsys, tt.pattern)
			}
		}
	}
}

func BenchmarkGlobWalk(b *testing.B) {
	fsys := os.DirFS("testdata")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, tt := range matchTests {
			if tt.isStandard && tt.testOnDisk {
				GlobWalk(fsys, tt.pattern, func(p string, d fs.DirEntry) error {
					return nil
				})
			}
		}
	}
}

func BenchmarkGoGlob(b *testing.B) {
	fsys := os.DirFS("testdata")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, tt := range matchTests {
			if tt.isStandard && tt.testOnDisk {
				fs.Glob(fsys, tt.pattern)
			}
		}
	}
}

func compareErrors(a, b error) bool {
	if a == nil {
		return b == nil
	}
	return b != nil
}

func inSlice(s string, a []string) bool {
	for _, i := range a {
		if i == s {
			return true
		}
	}
	return false
}

func compareSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	diff := make(map[string]int, len(a))

	for _, x := range a {
		diff[x]++
	}

	for _, y := range b {
		if _, ok := diff[y]; !ok {
			return false
		}

		diff[y]--
		if diff[y] == 0 {
			delete(diff, y)
		}
	}

	return len(diff) == 0
}

func buildNumResults() {
	testLen := len(matchTests)
	numResultsFilesOnly = make([]int, testLen)
	numResultsNoFollow = make([]int, testLen)
	numResultsNoHidden = make([]int, testLen)
	numResultsAllOpts = make([]int, testLen)

	fsys := os.DirFS("testdata")
	g := newGlob()
	for idx, tt := range matchTests {
		if tt.testOnDisk {
			filesOnly := 0
			noFollow := 0
			noHidden := 0
			allOpts := 0
			GlobWalk(fsys, tt.pattern, func(p string, d fs.DirEntry) error {
				isDir, _ := g.isDir(fsys, "", p, d)
				if !isDir {
					filesOnly++
				}

				hasNoFollow := (strings.HasPrefix(tt.pattern, "working-symlink") || !strings.Contains(p, "working-symlink/")) && !strings.Contains(p, "/symlink-dir/")
				if hasNoFollow {
					noFollow++
				}

				hasNoHidden := true
				if strings.Contains(tt.pattern, "hidden-tests") && !strings.Contains(tt.pattern, ".*") {
					hasNoHidden = (strings.Contains(tt.pattern, "hidden-dir") || !strings.Contains(p, "hidden-dir")) && !strings.Contains(p, "hidden-file")
				}
				if hasNoHidden {
					noHidden++
				}

				if hasNoFollow && hasNoHidden && (!isDir || p == "working-symlink") {
					allOpts++
				}

				return nil
			})

			numResultsFilesOnly[idx] = filesOnly
			numResultsNoFollow[idx] = noFollow
			numResultsNoHidden[idx] = noHidden
			numResultsAllOpts[idx] = allOpts
		}
	}
}

func mkdirp(parts ...string) string {
	dirs := path.Join(parts...)
	err := os.MkdirAll(dirs, 0755)
	if err != nil {
		log.Fatalf("Could not create test directories %v: %v\n", dirs, err)
	}
	return dirs
}

func touch(parts ...string) string {
	filename := path.Join(parts...)
	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("Could not create test file %v: %v\n", filename, err)
	}
	f.Close()
	return filename
}

func symlink(oldname, newname string) {
	// since this will only run on non-windows, we can assume "/" as path separator
	err := os.Symlink(oldname, newname)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("Could not create symlink %v -> %v: %v\n", oldname, newname, err)
	}
}

func exists(parts ...string) bool {
	p := path.Join(parts...)
	_, err := os.Lstat(p)
	return err == nil
}

func TestMain(m *testing.M) {
	// create the test directory
	mkdirp("testdata", "a", "b", "c")
	mkdirp("testdata", "a", "c")
	mkdirp("testdata", "abc")
	mkdirp("testdata", "axbxcxdxe", "xxx")
	mkdirp("testdata", "axbxcxdxexxx")
	mkdirp("testdata", "b")
	mkdirp("testdata", "e", "[x]", "[y]")
	mkdirp("testdata", "f", "{a,b}")
	mkdirp("testdata", "case-sensitive")

	// create test files
	touch("testdata", "1")
	touch("testdata", "a", "abc")
	touch("testdata", "a", "b", "c", "d")
	touch("testdata", "a", "c", "b")
	touch("testdata", "abc", "b")
	touch("testdata", "abcd")
	touch("testdata", "abcde")
	touch("testdata", "abxbbxdbxebxczzx")
	touch("testdata", "abxbbxdbxebxczzy")
	touch("testdata", "axbxcxdxe", "f")
	touch("testdata", "axbxcxdxe", "xxx", "f")
	touch("testdata", "axbxcxdxexxx", "f")
	touch("testdata", "axbxcxdxexxx", "fff")
	touch("testdata", "a☺b")
	touch("testdata", "b", "c")
	touch("testdata", "c")
	touch("testdata", "x")
	touch("testdata", "xxx")
	touch("testdata", "z")
	touch("testdata", "α")
	touch("testdata", "abc", "【test】.txt")

	touch("testdata", "e", "[")
	touch("testdata", "e", "]")
	touch("testdata", "e", "{")
	touch("testdata", "e", "}")
	touch("testdata", "e", "[]")
	touch("testdata", "e", "[x]", "[y]", "z")
	touch("testdata", "f", "{a,b}", "c")

	touch("testdata", "case-sensitive", "file")
	touch("testdata", "case-sensitive", "FILE")
	touch("testdata", "case-sensitive", "File")

	touch("testdata", "}")

	hiddenSubdir := mkdirpHidden("testdata", "hidden-tests", "hidden-dir")
	touchHidden(hiddenSubdir, "hidden-file")
	touch(hiddenSubdir, "visible-file")
	touchHidden("testdata", "hidden-tests", "hidden-file")
	touch("testdata", "hidden-tests", "visible-file")

	if !onWindows {
		// these files/symlinks won't work on Windows
		touch("testdata", "-")
		touch("testdata", "]")
		touch("testdata", "e", "*")
		touch("testdata", "e", "**")
		touch("testdata", "e", "****")
		touch("testdata", "e", "?")
		touch("testdata", "e", "\\")

		mkdirp("testdata", "f", "*")
		touch("testdata", "f", "*", "a")
		mkdirp("testdata", "f", "?")
		touch("testdata", "f", "?", "a")

		symlink("../axbxcxdxe/", "testdata/b/symlink-dir")
		symlink("/tmp/nonexistant-file-20160902155705", "testdata/broken-symlink")
		symlink("a/b", "testdata/working-symlink")

		if !exists("testdata", "nopermission") {
			mkdirp("testdata", "nopermission", "dir")
			touch("testdata", "nopermission", "file")
			os.Chmod(path.Join("testdata", "nopermission"), 0)
		}
	}

	// initialize numResultsFilesOnly
	buildNumResults()

	// We created three files with identical names in the `testdata/case-sensitive`
	// directory, only differing by case. If there's only one file in there, it's
	// because the filesystem is _not_ case-sensitive.
	fsys := os.DirFS("testdata")
	matches, err := fs.Glob(fsys, "case-sensitive/*")
	if err != nil {
		os.Exit(1)
	}
	fsIsCaseSensitive = len(matches) == 3

	os.Exit(m.Run())
}
