//go:build !js && !appengine
// +build !js,!appengine

package runewidth

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"testing"
	"unicode/utf8"
)

var _ sort.Interface = (*table)(nil) // ensure that type "table" does implement sort.Interface

func init() {
	os.Setenv("RUNEWIDTH_EASTASIAN", "")
	handleEnv()
}

func (t table) Len() int {
	return len(t)
}

func (t table) Less(i, j int) bool {
	return t[i].first < t[j].first
}

func (t *table) Swap(i, j int) {
	(*t)[i], (*t)[j] = (*t)[j], (*t)[i]
}

type tableInfo struct {
	tbl     table
	name    string
	wantN   int
	wantSHA string
}

var tables = []tableInfo{
	{private, "private", 137468, "a4a641206dc8c5de80bd9f03515a54a706a5a4904c7684dc6a33d65c967a51b2"},
	{nonprint, "nonprint", 2143, "288904683eb225e7c4c0bd3ee481b53e8dace404ec31d443afdbc4d13729fe95"},
	{combining, "combining", 842, "a89cdf3daf597a953ca315f005316f19d52a0f83197207fe6b844f295cbade54"},
	{doublewidth, "doublewidth", 182876, "55dcb1b999d6356d1a083085bb053bdeafc6dda05dec002617d85fda2a82d496"},
	{ambiguous, "ambiguous", 138483, "f4ed2dd733c0821cf6297fc24be0baea527ec7cad10d23b0ac7f57dcdf344cdb"},
	{emoji, "emoji", 2846, "09914b87febaa5493f2420a58f03dd6b026fa665b7c811abc7423a26a9b442c3"},
	{narrow, "narrow", 111, "fa897699c5e3cd9141c638d539331b0bdd508b874e22996c5e929767d455fc5a"},
	{neutral, "neutral", 33695, "f6af4edbdfe84d0c4c4419da8e2f86e49248010bad19c4ed7bf1897696db9084"},
}

func TestTableChecksums(t *testing.T) {
	for _, ti := range tables {
		gotN := 0
		buf := make([]byte, utf8.MaxRune+1)
		for r := rune(0); r <= utf8.MaxRune; r++ {
			if inTable(r, ti.tbl) {
				gotN++
				buf[r] = 1
			}
		}
		gotSHA := fmt.Sprintf("%x", sha256.Sum256(buf))
		if gotN != ti.wantN || gotSHA != ti.wantSHA {
			t.Errorf("table = %s,\n\tn = %d want %d,\n\tsha256 = %s want %s", ti.name, gotN, ti.wantN, gotSHA, ti.wantSHA)
		}
	}
}

func TestRuneWidthChecksums(t *testing.T) {
	var testcases = []struct {
		name           string
		eastAsianWidth bool
		wantSHA        string
	}{
		{"ea-no", false, "27fcc4f23635fb16651f9596517792cd497d4ac986020409215936e104d9eaa7"},
		{"ea-yes", true, "587ef2296285b71e9a37252a2f829db3caf85eddc2152bc6b22203bc2bcad01f"},
	}

	for _, testcase := range testcases {
		c := NewCondition()
		c.EastAsianWidth = testcase.eastAsianWidth
		buf := make([]byte, utf8.MaxRune+1)
		for r := rune(0); r <= utf8.MaxRune; r++ {
			buf[r] = byte(c.RuneWidth(r))
		}
		gotSHA := fmt.Sprintf("%x", sha256.Sum256(buf))
		if gotSHA != testcase.wantSHA {
			t.Errorf("TestRuneWidthChecksums = %s,\n\tsha256 = %s want %s",
				testcase.name, gotSHA, testcase.wantSHA)
		}

		// Test with LUT
		c.CreateLUT()
		for r := rune(0); r <= utf8.MaxRune; r++ {
			buf[r] = byte(c.RuneWidth(r))
		}
		gotSHA = fmt.Sprintf("%x", sha256.Sum256(buf))
		if gotSHA != testcase.wantSHA {
			t.Errorf("TestRuneWidthChecksums = %s,\n\tsha256 = %s want %s",
				testcase.name, gotSHA, testcase.wantSHA)
		}
	}
}

func TestDefaultLUT(t *testing.T) {
	var testcases = []struct {
		name           string
		eastAsianWidth bool
		wantSHA        string
	}{
		{"ea-no", false, "27fcc4f23635fb16651f9596517792cd497d4ac986020409215936e104d9eaa7"},
		{"ea-yes", true, "587ef2296285b71e9a37252a2f829db3caf85eddc2152bc6b22203bc2bcad01f"},
	}

	old := os.Getenv("RUNEWIDTH_EASTASIAN")
	defer os.Setenv("RUNEWIDTH_EASTASIAN", old)

	CreateLUT()
	for _, testcase := range testcases {
		c := DefaultCondition

		if testcase.eastAsianWidth {
			os.Setenv("RUNEWIDTH_EASTASIAN", "1")
		} else {
			os.Setenv("RUNEWIDTH_EASTASIAN", "0")
		}
		handleEnv()

		buf := make([]byte, utf8.MaxRune+1)
		for r := rune(0); r <= utf8.MaxRune; r++ {
			buf[r] = byte(c.RuneWidth(r))
		}
		gotSHA := fmt.Sprintf("%x", sha256.Sum256(buf))
		if gotSHA != testcase.wantSHA {
			t.Errorf("TestRuneWidthChecksums = %s,\n\tsha256 = %s want %s",
				testcase.name, gotSHA, testcase.wantSHA)
		}
	}
	// Remove for other tests.
	DefaultCondition.combinedLut = nil
}

func checkInterval(first, last rune) bool {
	return first >= 0 && first <= utf8.MaxRune &&
		last >= 0 && last <= utf8.MaxRune &&
		first <= last
}

func isCompact(t *testing.T, ti *tableInfo) bool {
	tbl := ti.tbl
	for i := range tbl {
		e := tbl[i]
		if !checkInterval(e.first, e.last) { // sanity check
			t.Errorf("table invalid: table = %s index = %d %v", ti.name, i, e)
			return false
		}
		if i+1 < len(tbl) && e.last+1 >= tbl[i+1].first { // can be combined into one entry
			t.Errorf("table not compact: table = %s index = %d %v %v", ti.name, i, e, tbl[i+1])
			return false
		}
	}
	return true
}

func TestSorted(t *testing.T) {
	for _, ti := range tables {
		if !sort.IsSorted(&ti.tbl) {
			t.Errorf("table not sorted: %s", ti.name)
		}
		if !isCompact(t, &ti) {
			t.Errorf("table not compact: %s", ti.name)
		}
	}
}

var runewidthtests = []struct {
	in     rune
	out    int
	eaout  int
	nseout int
}{
	{'世', 2, 2, 2},
	{'界', 2, 2, 2},
	{'ｾ', 1, 1, 1},
	{'ｶ', 1, 1, 1},
	{'ｲ', 1, 1, 1},
	{'☆', 1, 2, 2}, // double width in ambiguous
	{'☺', 1, 1, 2},
	{'☻', 1, 1, 1},
	{'♥', 1, 2, 2},
	{'♦', 1, 1, 2},
	{'♣', 1, 2, 2},
	{'♠', 1, 2, 2},
	{'♂', 1, 2, 2},
	{'♀', 1, 2, 2},
	{'♪', 1, 2, 2},
	{'♫', 1, 1, 1},
	{'☼', 1, 1, 1},
	{'↕', 1, 2, 2},
	{'‼', 1, 1, 2},
	{'↔', 1, 2, 2},
	{'\x00', 0, 0, 0},
	{'\x01', 0, 0, 0},
	{'\u0300', 0, 0, 0},
	{'\u2028', 0, 0, 0},
	{'\u2029', 0, 0, 0},
	{'a', 1, 1, 1}, // ASCII classified as "na" (narrow)
	{'⟦', 1, 1, 1}, // non-ASCII classified as "na" (narrow)
	{'👁', 1, 1, 2},
}

func TestRuneWidth(t *testing.T) {
	c := NewCondition()
	c.EastAsianWidth = false
	for _, tt := range runewidthtests {
		if out := c.RuneWidth(tt.in); out != tt.out {
			t.Errorf("RuneWidth(%q) = %d, want %d (EastAsianWidth=false)", tt.in, out, tt.out)
		}
	}
	c.EastAsianWidth = true
	for _, tt := range runewidthtests {
		if out := c.RuneWidth(tt.in); out != tt.eaout {
			t.Errorf("RuneWidth(%q) = %d, want %d (EastAsianWidth=true)", tt.in, out, tt.eaout)
		}
	}
	c.StrictEmojiNeutral = false
	for _, tt := range runewidthtests {
		if out := c.RuneWidth(tt.in); out != tt.nseout {
			t.Errorf("RuneWidth(%q) = %d, want %d (StrictEmojiNeutral=false)", tt.in, out, tt.eaout)
		}
	}
}

var isambiguouswidthtests = []struct {
	in  rune
	out bool
}{
	{'世', false},
	{'■', true},
	{'界', false},
	{'○', true},
	{'㈱', false},
	{'①', true},
	{'②', true},
	{'③', true},
	{'④', true},
	{'⑤', true},
	{'⑥', true},
	{'⑦', true},
	{'⑧', true},
	{'⑨', true},
	{'⑩', true},
	{'⑪', true},
	{'⑫', true},
	{'⑬', true},
	{'⑭', true},
	{'⑮', true},
	{'⑯', true},
	{'⑰', true},
	{'⑱', true},
	{'⑲', true},
	{'⑳', true},
	{'☆', true},
}

func TestIsAmbiguousWidth(t *testing.T) {
	for _, tt := range isambiguouswidthtests {
		if out := IsAmbiguousWidth(tt.in); out != tt.out {
			t.Errorf("IsAmbiguousWidth(%q) = %v, want %v", tt.in, out, tt.out)
		}
	}
}

var stringwidthtests = []struct {
	in    string
	out   int
	eaout int
}{
	{"■㈱の世界①", 10, 12},
	{"スター☆", 7, 8},
	{"つのだ☆HIRO", 11, 12},
}

func TestStringWidth(t *testing.T) {
	c := NewCondition()
	c.EastAsianWidth = false
	for _, tt := range stringwidthtests {
		if out := c.StringWidth(tt.in); out != tt.out {
			t.Errorf("StringWidth(%q) = %d, want %d", tt.in, out, tt.out)
		}
	}
	c.EastAsianWidth = true
	for _, tt := range stringwidthtests {
		if out := c.StringWidth(tt.in); out != tt.eaout {
			t.Errorf("StringWidth(%q) = %d, want %d (EA)", tt.in, out, tt.eaout)
		}
	}
}

func TestStringWidthInvalid(t *testing.T) {
	s := "こんにちわ\x00世界"
	if out := StringWidth(s); out != 14 {
		t.Errorf("StringWidth(%q) = %d, want %d", s, out, 14)
	}
}

func TestTruncateSmaller(t *testing.T) {
	s := "あいうえお"
	expected := "あいうえお"

	if out := Truncate(s, 10, "..."); out != expected {
		t.Errorf("Truncate(%q) = %q, want %q", s, out, expected)
	}
}

func TestTruncate(t *testing.T) {
	s := "あいうえおあいうえおえおおおおおおおおおおおおおおおおおおおおおおおおおおおおおお"
	expected := "あいうえおあいうえおえおおおおおおおおおおおおおおおおおおおおおおおおおおお..."
	out := Truncate(s, 80, "...")
	if out != expected {
		t.Errorf("Truncate(%q) = %q, want %q", s, out, expected)
	}
	width := StringWidth(out)
	if width != 79 {
		t.Errorf("width of Truncate(%q) should be %d, but %d", s, 79, width)
	}
}

func TestTruncateFit(t *testing.T) {
	s := "aあいうえおあいうえおえおおおおおおおおおおおおおおおおおおおおおおおおおおおおおお"
	expected := "aあいうえおあいうえおえおおおおおおおおおおおおおおおおおおおおおおおおおおお..."

	out := Truncate(s, 80, "...")
	if out != expected {
		t.Errorf("Truncate(%q) = %q, want %q", s, out, expected)
	}
	width := StringWidth(out)
	if width != 80 {
		t.Errorf("width of Truncate(%q) should be %d, but %d", s, 80, width)
	}
}

func TestTruncateJustFit(t *testing.T) {
	s := "あいうえおあいうえおえおおおおおおおおおおおおおおおおおおおおおおおおおおおおお"
	expected := "あいうえおあいうえおえおおおおおおおおおおおおおおおおおおおおおおおおおおおおお"

	out := Truncate(s, 80, "...")
	if out != expected {
		t.Errorf("Truncate(%q) = %q, want %q", s, out, expected)
	}
	width := StringWidth(out)
	if width != 80 {
		t.Errorf("width of Truncate(%q) should be %d, but %d", s, 80, width)
	}
}

func TestWrap(t *testing.T) {
	s := `東京特許許可局局長はよく柿喰う客だ/東京特許許可局局長はよく柿喰う客だ
123456789012345678901234567890

END`
	expected := `東京特許許可局局長はよく柿喰う
客だ/東京特許許可局局長はよく
柿喰う客だ
123456789012345678901234567890

END`

	if out := Wrap(s, 30); out != expected {
		t.Errorf("Wrap(%q) = %q, want %q", s, out, expected)
	}
}

func TestTruncateNoNeeded(t *testing.T) {
	s := "あいうえおあい"
	expected := "あいうえおあい"

	if out := Truncate(s, 80, "..."); out != expected {
		t.Errorf("Truncate(%q) = %q, want %q", s, out, expected)
	}
}

var truncatelefttests = []struct {
	s      string
	w      int
	prefix string
	out    string
}{
	{"source", 4, "", "ce"},
	{"source", 4, "...", "...ce"},
	{"あいうえお", 6, "", "えお"},
	{"あいうえお", 6, "...", "...えお"},
	{"あいうえお", 10, "", ""},
	{"あいうえお", 10, "...", "..."},
	{"あいうえお", 5, "", " えお"},
	{"Aあいうえお", 5, "", "うえお"},
}

func TestTruncateLeft(t *testing.T) {
	t.Parallel()

	for _, tt := range truncatelefttests {
		if out := TruncateLeft(tt.s, tt.w, tt.prefix); out != tt.out {
			t.Errorf("TruncateLeft(%q) = %q, want %q", tt.s, out, tt.out)
		}
	}
}

var isneutralwidthtests = []struct {
	in  rune
	out bool
}{
	{'→', false},
	{'┊', false},
	{'┈', false},
	{'～', false},
	{'└', false},
	{'⣀', true},
	{'⣀', true},
}

func TestIsNeutralWidth(t *testing.T) {
	for _, tt := range isneutralwidthtests {
		if out := IsNeutralWidth(tt.in); out != tt.out {
			t.Errorf("IsNeutralWidth(%q) = %v, want %v", tt.in, out, tt.out)
		}
	}
}

var iscombiningwidthtests = []struct {
	in  rune
	out bool
}{
	{'\u0300', true},  // COMBINING GRAVE ACCENT
	{'\uFE0F', true},  // VARIATION SELECTOR-16
	{'\uFE0E', true},  // VARIATION SELECTOR-15
	{'\u180B', true},  // MONGOLIAN FREE VARIATION SELECTOR ONE
	{'\U000E0100', true}, // VARIATION SELECTOR-17
	{'A', false},
	{'☆', false},
}

func TestIsCombiningWidth(t *testing.T) {
	for _, tt := range iscombiningwidthtests {
		if out := IsCombiningWidth(tt.in); out != tt.out {
			t.Errorf("IsCombiningWidth(%q) = %v, want %v", tt.in, out, tt.out)
		}
	}
}

func TestFillLeft(t *testing.T) {
	s := "あxいうえお"
	expected := "    あxいうえお"

	if out := FillLeft(s, 15); out != expected {
		t.Errorf("FillLeft(%q) = %q, want %q", s, out, expected)
	}
}

func TestFillLeftFit(t *testing.T) {
	s := "あいうえお"
	expected := "あいうえお"

	if out := FillLeft(s, 10); out != expected {
		t.Errorf("FillLeft(%q) = %q, want %q", s, out, expected)
	}
}

func TestFillRight(t *testing.T) {
	s := "あxいうえお"
	expected := "あxいうえお    "

	if out := FillRight(s, 15); out != expected {
		t.Errorf("FillRight(%q) = %q, want %q", s, out, expected)
	}
}

func TestFillRightFit(t *testing.T) {
	s := "あいうえお"
	expected := "あいうえお"

	if out := FillRight(s, 10); out != expected {
		t.Errorf("FillRight(%q) = %q, want %q", s, out, expected)
	}
}

func TestEnv(t *testing.T) {
	old := os.Getenv("RUNEWIDTH_EASTASIAN")
	defer os.Setenv("RUNEWIDTH_EASTASIAN", old)

	os.Setenv("RUNEWIDTH_EASTASIAN", "0")
	handleEnv()

	if w := RuneWidth('│'); w != 1 {
		t.Errorf("RuneWidth('│') = %d, want %d", w, 1)
	}
}

func TestZeroWidthJoiner(t *testing.T) {
	c := NewCondition()

	var tests = []struct {
		in   string
		want int
	}{
		{"👩", 2},
		{"👩\u200d", 2},
		{"👩\u200d🍳", 2},
		{"\u200d🍳", 2},
		{"👨\u200d👨", 2},
		{"👨\u200d👨\u200d👧", 2},
		{"🏳️\u200d🌈", 1},
		{"あ👩\u200d🍳い", 6},
		{"あ\u200d🍳い", 6},
		{"あ\u200dい", 4},
	}

	for _, tt := range tests {
		if got := c.StringWidth(tt.in); got != tt.want {
			t.Errorf("StringWidth(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
