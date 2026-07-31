package regexp2

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dlclark/regexp2/syntax"
)

func TestBacktrack_CatastrophicTimeout(t *testing.T) {
	r, err := Compile("(.+)*\\?", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("code dump: %v", r.code.Dump())
	const subject = "Do you think you found the problem string!"

	const earlyAllowance = 10 * time.Millisecond
	var lateAllowance = clockPeriod + 500*time.Millisecond // Large allowance in case machine is slow

	for _, timeout := range []time.Duration{
		-1 * time.Millisecond,
		0 * time.Millisecond,
		1 * time.Millisecond,
		10 * time.Millisecond,
		100 * time.Millisecond,
		500 * time.Millisecond,
		1000 * time.Millisecond,
	} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			r.MatchTimeout = timeout
			start := time.Now()
			m, err := r.FindStringMatch(subject)
			elapsed := time.Since(start)
			if err == nil {
				t.Errorf("expected timeout err")
			}
			if m != nil {
				t.Errorf("Expected no match")
			}
			t.Logf("timeed out after %v", elapsed)
			if elapsed < timeout-earlyAllowance {
				t.Errorf("Match timed out too quickly (%v instead of expected %v)", elapsed, timeout-earlyAllowance)
			}
			if elapsed > timeout+lateAllowance {
				t.Errorf("Match timed out too late (%v instead of expected %v)", elapsed, timeout+lateAllowance)
			}
		})
	}
}

func TestSetPrefix(t *testing.T) {
	r := MustCompile(`^\s*-TEST`, 0)
	if r.code.FcPrefix == nil {
		t.Fatalf("Expected prefix set [-\\s] but was nil")
	}
	if r.code.FcPrefix.PrefixSet.String() != "[-\\s]" {
		t.Fatalf("Expected prefix set [\\s-] but was %v", r.code.FcPrefix.PrefixSet.String())
	}
}

func TestSetInCode(t *testing.T) {
	r := MustCompile(`(?<body>\s*(?<name>.+))`, 0)
	t.Logf("code dump: %v", r.code.Dump())
	if want, got := 1, len(r.code.Sets); want != got {
		t.Fatalf("r.code.Sets wanted %v, got %v", want, got)
	}
	if want, got := "[\\s]", r.code.Sets[0].String(); want != got {
		t.Fatalf("first set wanted %v, got %v", want, got)
	}
}

func TestRegexp_Basic(t *testing.T) {
	r, err := Compile("test(?<named>ing)?", 0)
	//t.Logf("code dump: %v", r.code.Dump())

	if err != nil {
		t.Errorf("unexpected compile err: %v", err)
	}
	m, err := r.FindStringMatch("this is a testing stuff")
	if err != nil {
		t.Errorf("unexpected match err: %v", err)
	}
	if m == nil {
		t.Error("Nil match, expected success")
	} else {
		//t.Logf("Match: %v", m.dump())
	}
}

// check all our functions and properties around basic capture groups and referential for Group 0
func TestCapture_Basic(t *testing.T) {
	r := MustCompile(`.*\B(SUCCESS)\B.*`, 0)
	m, err := r.FindStringMatch("adfadsfSUCCESSadsfadsf")
	if err != nil {
		t.Fatalf("Unexpected match error: %v", err)
	}

	if m == nil {
		t.Fatalf("Should have matched")
	}
	if want, got := "adfadsfSUCCESSadsfadsf", m.String(); want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	if want, got := 0, m.Index; want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	if want, got := 22, m.Length; want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	if want, got := 1, len(m.Captures); want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}

	if want, got := m.String(), m.Captures[0].String(); want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	if want, got := 0, m.Captures[0].Index; want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	if want, got := 22, m.Captures[0].Length; want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}

	g := m.Groups()
	if want, got := 2, len(g); want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	// group 0 is always the match
	if want, got := m.String(), g[0].String(); want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	if want, got := 1, len(g[0].Captures); want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	// group 0's capture is always the match
	if want, got := m.Captures[0].String(), g[0].Captures[0].String(); want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}

	// group 1 is our first explicit group (unnamed)
	if want, got := 7, g[1].Index; want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	if want, got := 7, g[1].Length; want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
	if want, got := "SUCCESS", g[1].String(); want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}
}

func TestEscapeUnescape_Basic(t *testing.T) {
	s1 := "#$^*+(){}<>\\|. "
	s2 := Escape(s1)
	s3, err := Unescape(s2)
	if err != nil {
		t.Fatalf("Unexpected error during unescape: %v", err)
	}

	//confirm one way
	if want, got := `\#\$\^\*\+\(\)\{\}<>\\\|\.\ `, s2; want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}

	//confirm round-trip
	if want, got := s1, s3; want != got {
		t.Fatalf("Wanted '%v'\nGot '%v'", want, got)
	}

}

func TestGroups_Basic(t *testing.T) {
	type d struct {
		p    string
		s    string
		name []string
		num  []int
		strs []string
	}
	data := []d{
		d{"(?<first_name>\\S+)\\s(?<last_name>\\S+)", // example
			"Ryan Byington",
			[]string{"0", "first_name", "last_name"},
			[]int{0, 1, 2},
			[]string{"Ryan Byington", "Ryan", "Byington"}},
		d{"((?<One>abc)\\d+)?(?<Two>xyz)(.*)", // example
			"abc208923xyzanqnakl",
			[]string{"0", "1", "2", "One", "Two"},
			[]int{0, 1, 2, 3, 4},
			[]string{"abc208923xyzanqnakl", "abc208923", "anqnakl", "abc", "xyz"}},
		d{"((?<256>abc)\\d+)?(?<16>xyz)(.*)", // numeric names
			"0272saasdabc8978xyz][]12_+-",
			[]string{"0", "1", "2", "16", "256"},
			[]int{0, 1, 2, 16, 256},
			[]string{"abc8978xyz][]12_+-", "abc8978", "][]12_+-", "xyz", "abc"}},
		d{"((?<4>abc)(?<digits>\\d+))?(?<2>xyz)(?<everything_else>.*)", // mix numeric and string names
			"0272saasdabc8978xyz][]12_+-",
			[]string{"0", "1", "2", "digits", "4", "everything_else"},
			[]int{0, 1, 2, 3, 4, 5},
			[]string{"abc8978xyz][]12_+-", "abc8978", "xyz", "8978", "abc", "][]12_+-"}},
		d{"(?<first_name>\\S+)\\s(?<first_name>\\S+)", // dupe string names
			"Ryan Byington",
			[]string{"0", "first_name"},
			[]int{0, 1},
			[]string{"Ryan Byington", "Byington"}},
		d{"(?<15>\\S+)\\s(?<15>\\S+)", // dupe numeric names
			"Ryan Byington",
			[]string{"0", "15"},
			[]int{0, 15},
			[]string{"Ryan Byington", "Byington"}},
		// *** repeated from above, but with alt cap syntax ***
		d{"(?'first_name'\\S+)\\s(?'last_name'\\S+)", //example
			"Ryan Byington",
			[]string{"0", "first_name", "last_name"},
			[]int{0, 1, 2},
			[]string{"Ryan Byington", "Ryan", "Byington"}},
		d{"((?'One'abc)\\d+)?(?'Two'xyz)(.*)", // example
			"abc208923xyzanqnakl",
			[]string{"0", "1", "2", "One", "Two"},
			[]int{0, 1, 2, 3, 4},
			[]string{"abc208923xyzanqnakl", "abc208923", "anqnakl", "abc", "xyz"}},
		d{"((?'256'abc)\\d+)?(?'16'xyz)(.*)", // numeric names
			"0272saasdabc8978xyz][]12_+-",
			[]string{"0", "1", "2", "16", "256"},
			[]int{0, 1, 2, 16, 256},
			[]string{"abc8978xyz][]12_+-", "abc8978", "][]12_+-", "xyz", "abc"}},
		d{"((?'4'abc)(?'digits'\\d+))?(?'2'xyz)(?'everything_else'.*)", // mix numeric and string names
			"0272saasdabc8978xyz][]12_+-",
			[]string{"0", "1", "2", "digits", "4", "everything_else"},
			[]int{0, 1, 2, 3, 4, 5},
			[]string{"abc8978xyz][]12_+-", "abc8978", "xyz", "8978", "abc", "][]12_+-"}},
		d{"(?'first_name'\\S+)\\s(?'first_name'\\S+)", // dupe string names
			"Ryan Byington",
			[]string{"0", "first_name"},
			[]int{0, 1},
			[]string{"Ryan Byington", "Byington"}},
		d{"(?'15'\\S+)\\s(?'15'\\S+)", // dupe numeric names
			"Ryan Byington",
			[]string{"0", "15"},
			[]int{0, 15},
			[]string{"Ryan Byington", "Byington"}},
	}

	fatalf := func(re *Regexp, v d, format string, args ...interface{}) {
		args = append(args, v, re.code.Dump())

		t.Fatalf(format+" using test data: %#v\ndump:%v", args...)
	}

	validateGroupNamesNumbers := func(re *Regexp, v d) {
		if len(v.name) != len(v.num) {
			fatalf(re, v, "Invalid data, group name count and number count must match")
		}

		groupNames := re.GetGroupNames()
		if !reflect.DeepEqual(groupNames, v.name) {
			fatalf(re, v, "group names expected: %v, actual: %v", v.name, groupNames)
		}
		groupNums := re.GetGroupNumbers()
		if !reflect.DeepEqual(groupNums, v.num) {
			fatalf(re, v, "group numbers expected: %v, actual: %v", v.num, groupNums)
		}
		// make sure we can freely get names and numbers from eachother
		for i := range groupNums {
			if want, got := groupNums[i], re.GroupNumberFromName(groupNames[i]); want != got {
				fatalf(re, v, "group num from name Wanted '%v'\nGot '%v'", want, got)
			}
			if want, got := groupNames[i], re.GroupNameFromNumber(groupNums[i]); want != got {
				fatalf(re, v, "group name from num Wanted '%v'\nGot '%v'", want, got)
			}
		}
	}

	for _, v := range data {
		// compile the regex
		re := MustCompile(v.p, 0)

		// validate our group name/num info before execute
		validateGroupNamesNumbers(re, v)

		m, err := re.FindStringMatch(v.s)
		if err != nil {
			fatalf(re, v, "Unexpected error in match: %v", err)
		}
		if m == nil {
			fatalf(re, v, "Match is nil")
		}
		if want, got := len(v.strs), m.GroupCount(); want != got {
			fatalf(re, v, "GroupCount() Wanted '%v'\nGot '%v'", want, got)
		}
		g := m.Groups()
		if want, got := len(v.strs), len(g); want != got {
			fatalf(re, v, "len(m.Groups()) Wanted '%v'\nGot '%v'", want, got)
		}
		// validate each group's value from the execute
		for i := range v.name {
			grp1 := m.GroupByName(v.name[i])
			grp2 := m.GroupByNumber(v.num[i])
			// should be identical reference
			if grp1 != grp2 {
				fatalf(re, v, "Expected GroupByName and GroupByNumber to return same result for %v, %v", v.name[i], v.num[i])
			}
			if want, got := v.strs[i], grp1.String(); want != got {
				fatalf(re, v, "Value[%v] Wanted '%v'\nGot '%v'", i, want, got)
			}
		}

		// validate our group name/num info after execute
		validateGroupNamesNumbers(re, v)
	}
}

func TestErr_GroupName(t *testing.T) {
	// group 0 is off limits
	if _, err := Compile("foo(?<0>bar)", 0); err == nil {
		t.Fatalf("zero group, expected error during compile")
	} else if want, got := "error parsing regexp: capture number cannot be zero in `foo(?<0>bar)`", err.Error(); want != got {
		t.Fatalf("invalid error text, want '%v', got '%v'", want, got)
	}
	if _, err := Compile("foo(?'0'bar)", 0); err == nil {
		t.Fatalf("zero group, expected error during compile")
	} else if want, got := "error parsing regexp: capture number cannot be zero in `foo(?'0'bar)`", err.Error(); want != got {
		t.Fatalf("invalid error text, want '%v', got '%v'", want, got)
	}

	// group tag can't start with a num
	if _, err := Compile("foo(?<1bar>)", 0); err == nil {
		t.Fatalf("invalid group name, expected error during compile")
	} else if want, got := "error parsing regexp: invalid group name: group names must begin with a word character and have a matching terminator in `foo(?<1bar>)`", err.Error(); want != got {
		t.Fatalf("invalid error text, want '%v', got '%v'", want, got)
	}
	if _, err := Compile("foo(?'1bar')", 0); err == nil {
		t.Fatalf("invalid group name, expected error during compile")
	} else if want, got := "error parsing regexp: invalid group name: group names must begin with a word character and have a matching terminator in `foo(?'1bar')`", err.Error(); want != got {
		t.Fatalf("invalid error text, want '%v', got '%v'", want, got)
	}

	// missing closing group tag
	if _, err := Compile("foo(?<bar)", 0); err == nil {
		t.Fatalf("invalid group name, expected error during compile")
	} else if want, got := "error parsing regexp: invalid group name: group names must begin with a word character and have a matching terminator in `foo(?<bar)`", err.Error(); want != got {
		t.Fatalf("invalid error text, want '%v', got '%v'", want, got)
	}
	if _, err := Compile("foo(?'bar)", 0); err == nil {
		t.Fatalf("invalid group name, expected error during compile")
	} else if want, got := "error parsing regexp: invalid group name: group names must begin with a word character and have a matching terminator in `foo(?'bar)`", err.Error(); want != got {
		t.Fatalf("invalid error text, want '%v', got '%v'", want, got)
	}

}

func TestConstantUneffected(t *testing.T) {
	// had a bug where "constant" sets would get modified with alternations and be broken in memory until restart
	// this meant that if you used a known-set (like \s) in a larger set it would "poison" \s for the process
	re := MustCompile(`(\s|\*)test\s`, 0)
	if want, got := 2, len(re.code.Sets); want != got {
		t.Fatalf("wanted %v sets, got %v", want, got)
	}
	if want, got := "[\\*\\s]", re.code.Sets[0].String(); want != got {
		t.Fatalf("wanted set 0 %v, got %v", want, got)
	}
	if want, got := "[\\s]", re.code.Sets[1].String(); want != got {
		t.Fatalf("wanted set 1 %v, got %v", want, got)
	}
}

func TestAlternationConstAndEscape(t *testing.T) {
	re := MustCompile(`\:|\s`, 0)
	if want, got := 1, len(re.code.Sets); want != got {
		t.Fatalf("wanted %v sets, got %v", want, got)
	}
	if want, got := "[:\\s]", re.code.Sets[0].String(); want != got {
		t.Fatalf("wanted set 0 %v, got %v", want, got)
	}
}

func TestStartingCharsOptionalNegate(t *testing.T) {
	// to maintain matching with the corefx we've made the negative char classes be negative and the
	// categories they contain positive.  This means they're not combinable or suitable for prefixes.
	// In general this could be a fine thing since negatives are extremely wide groups and not
	// missing much on prefix optimizations.

	// the below expression *could* have a prefix of [\S\d] but
	// this requires a change in charclass.go when setting
	// NotSpaceClass = getCharSetFromCategoryString()
	// to negate the individual categories rather than the CharSet itself
	// this would deviate from corefx

	re := MustCompile(`(^(\S{2} )?\S{2}(\d+|/) *\S{3}\S{3} ?\d{2,4}[A-Z] ?\d{2}[A-Z]{3}|(\S{2} )?\d{2,4})`, 0)
	if re.code.FcPrefix != nil {
		t.Fatalf("FcPrefix wanted nil, got %v", re.code.FcPrefix)
	}
}

func TestParseNegativeDigit(t *testing.T) {
	re := MustCompile(`\D`, 0)
	if want, got := 1, len(re.code.Sets); want != got {
		t.Fatalf("wanted %v sets, got %v", want, got)
	}

	if want, got := "[\\P{Nd}]", re.code.Sets[0].String(); want != got {
		t.Fatalf("wanted set 0 %v, got %v", want, got)
	}
}

func TestRunNegativeDigit(t *testing.T) {
	re := MustCompile(`\D`, 0)
	m, err := re.MatchString("this is a test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !m {
		t.Fatalf("Expected match")
	}
}

func TestCancellingClasses(t *testing.T) {
	// [\w\W\s] should become "." because it means "anything"
	re := MustCompile(`[\w\W\s]`, 0)
	if want, got := 1, len(re.code.Sets); want != got {
		t.Fatalf("wanted %v sets, got %v", want, got)
	}
	if want, got := syntax.AnyClass().String(), re.code.Sets[0].String(); want != got {
		t.Fatalf("wanted set 0 %v, got %v", want, got)
	}
}

func TestConcatLoopCaptureSet(t *testing.T) {
	//(A|B)*?CD different Concat/Loop/Capture/Set (had [A-Z] should be [AB])
	// we were not copying the Sets in the prefix FC stack, so the underlying sets were unexpectedly mutating
	// so set [AB] becomes [ABC] when we see the the static C in FC stack generation (which are the valid start chars),
	// but that was mutating the tree node's original set [AB] because even though we copied the slie header,
	// the two header's pointed to the same underlying byte array...which was mutated.

	re := MustCompile(`(A|B)*CD`, 0)
	if want, got := 1, len(re.code.Sets); want != got {
		t.Fatalf("wanted %v sets, got %v", want, got)
	}
	if want, got := "[AB]", re.code.Sets[0].String(); want != got {
		t.Fatalf("wanted set 0 %v, got %v", want, got)
	}
}

func TestFirstcharsIgnoreCase(t *testing.T) {
	//((?i)AB(?-i)C|D)E different Firstchars (had [da] should be [ad])
	// we were not canonicalizing when converting the prefix set to lower case
	// so our set's were potentially not searching properly
	re := MustCompile(`((?i)AB(?-i)C|D)E`, 0)

	if re.code.FcPrefix == nil {
		t.Fatalf("wanted prefix, got nil")
	}

	if want, got := "[ad]", re.code.FcPrefix.PrefixSet.String(); want != got {
		t.Fatalf("wanted prefix %v, got %v", want, got)
	}
}

func TestRepeatingGroup(t *testing.T) {
	re := MustCompile(`(data?)+`, 0)

	m, err := re.FindStringMatch("datadat")
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}

	if m == nil {
		t.Fatalf("Expected match")
	}

	g := m.GroupByNumber(1)
	if g == nil {
		t.Fatalf("Expected group")
	}

	if want, got := 2, len(g.Captures); want != got {
		t.Fatalf("wanted cap count %v, got %v", want, got)
	}

	if want, got := g.Captures[1].String(), g.Capture.String(); want != got {
		t.Fatalf("expected last capture of the group to be embedded")
	}

	if want, got := "data", g.Captures[0].String(); want != got {
		t.Fatalf("expected cap 0 to be %v, got %v", want, got)
	}
	if want, got := "dat", g.Captures[1].String(); want != got {
		t.Fatalf("expected cap 1 to be %v, got %v", want, got)
	}

}

func TestFindNextMatch_Basic(t *testing.T) {
	re := MustCompile(`(T|E)(?=h|E|S|$)`, 0)
	m, err := re.FindStringMatch(`This is a TEST`)
	if err != nil {
		t.Fatalf("Unexpected err 0: %v", err)
	}
	if m == nil {
		t.Fatalf("Expected match 0")
	}
	if want, got := 0, m.Index; want != got {
		t.Fatalf("expected match 0 to start at %v, got %v", want, got)
	}

	m, err = re.FindNextMatch(m)
	if err != nil {
		t.Fatalf("Unexpected err 1: %v", err)
	}
	if m == nil {
		t.Fatalf("Expected match 1")
	}
	if want, got := 10, m.Index; want != got {
		t.Fatalf("expected match 1 to start at %v, got %v", want, got)
	}

	m, err = re.FindNextMatch(m)
	if err != nil {
		t.Fatalf("Unexpected err 2: %v", err)
	}
	if m == nil {
		t.Fatalf("Expected match 2")
	}
	if want, got := 11, m.Index; want != got {
		t.Fatalf("expected match 2 to start at %v, got %v", want, got)
	}

	m, err = re.FindNextMatch(m)
	if err != nil {
		t.Fatalf("Unexpected err 3: %v", err)
	}
	if m == nil {
		t.Fatalf("Expected match 3")
	}
	if want, got := 13, m.Index; want != got {
		t.Fatalf("expected match 3 to start at %v, got %v", want, got)
	}
}

func TestUnicodeSupplementaryCharSetMatch(t *testing.T) {
	//0x2070E 0x20731 𠜱 0x20779 𠝹
	re := MustCompile("[𠜎-𠝹]", 0)

	if m, err := re.MatchString("\u2070"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if m {
		t.Fatalf("Unexpected match")
	}

	if m, err := re.MatchString("𠜱"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}
}

func TestUnicodeSupplementaryCharInRange(t *testing.T) {
	//0x2070E 0x20731 𠜱 0x20779 𠝹
	re := MustCompile(".", 0)

	if m, err := re.MatchString("\u2070"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	if m, err := re.MatchString("𠜱"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}
}

func TestUnicodeScriptSets(t *testing.T) {
	re := MustCompile(`\p{Katakana}+`, 0)
	if m, err := re.MatchString("\u30A0\u30FF"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}
}

func TestHexadecimalCurlyBraces(t *testing.T) {
	re := MustCompile(`\x20`, 0)
	if m, err := re.MatchString(" "); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	re = MustCompile(`\x{C4}`, 0)
	if m, err := re.MatchString("Ä"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	re = MustCompile(`\x{0C5}`, 0)
	if m, err := re.MatchString("Å"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	re = MustCompile(`\x{00C6}`, 0)
	if m, err := re.MatchString("Æ"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	re = MustCompile(`\x{1FF}`, 0)
	if m, err := re.MatchString("ǿ"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	re = MustCompile(`\x{02FF}`, 0)
	if m, err := re.MatchString("˿"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	re = MustCompile(`\x{1392}`, 0)
	if m, err := re.MatchString("᎒"); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	re = MustCompile(`\x{0010ffff}`, 0)
	if m, err := re.MatchString(string(rune(0x10ffff))); err != nil {
		t.Fatalf("Unexpected err: %v", err)
	} else if !m {
		t.Fatalf("Expected match")
	}

	if _, err := Compile(`\x2R`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x0`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x{`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x{2`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x{2R`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x{2R}`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x{}`, 0); err == nil {
		t.Fatalf("Expected error")
	}
	if _, err := Compile(`\x{10000`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x{1234`, 0); err == nil {
		t.Fatal("Expected error")
	}
	if _, err := Compile(`\x{123456789}`, 0); err == nil {
		t.Fatal("Expected error")
	}

}

func TestEmptyCharClass(t *testing.T) {
	if _, err := Compile("[]", 0); err == nil {
		t.Fatal("Empty char class isn't valid outside of ECMAScript mode")
	}
}

func TestECMAEmptyCharClass(t *testing.T) {
	re := MustCompile("[]", ECMAScript)
	if m, err := re.MatchString("a"); err != nil {
		t.Fatal(err)
	} else if m {
		t.Fatal("Expected no match")
	}
}

func TestDot(t *testing.T) {
	re := MustCompile(".", 0)
	if m, err := re.MatchString("\r"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestECMADot(t *testing.T) {
	re := MustCompile(".", ECMAScript)
	if m, err := re.MatchString("\r"); err != nil {
		t.Fatal(err)
	} else if m {
		t.Fatal("Expected no match")
	}
}

func TestDecimalLookahead(t *testing.T) {
	re := MustCompile(`\1(A)`, 0)
	m, err := re.FindStringMatch("AA")
	if err != nil {
		t.Fatal(err)
	} else if m != nil {
		t.Fatal("Expected no match")
	}
}

func TestECMADecimalLookahead(t *testing.T) {
	re := MustCompile(`\1(A)`, ECMAScript)
	m, err := re.FindStringMatch("AA")
	if err != nil {
		t.Fatal(err)
	}

	if c := m.GroupCount(); c != 2 {
		t.Fatalf("Group count !=2 (%d)", c)
	}

	if s := m.GroupByNumber(0).String(); s != "A" {
		t.Fatalf("Group0 != 'A' ('%s')", s)
	}

	if s := m.GroupByNumber(1).String(); s != "A" {
		t.Fatalf("Group1 != 'A' ('%s')", s)
	}
}

func TestECMAOctal(t *testing.T) {
	re := MustCompile(`\100`, ECMAScript)
	if m, err := re.MatchString("@"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	if m, err := re.MatchString("x"); err != nil {
		t.Fatal(err)
	} else if m {
		t.Fatal("Expected no match")
	}

	re = MustCompile(`\377`, ECMAScript)
	if m, err := re.MatchString("\u00ff"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	re = MustCompile(`\400`, ECMAScript)
	if m, err := re.MatchString(" 0"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

}

func TestECMAInvalidEscape(t *testing.T) {
	re := MustCompile(`\x0`, ECMAScript)
	if m, err := re.MatchString("x0"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	re = MustCompile(`\x0z`, ECMAScript)
	if m, err := re.MatchString("x0z"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestECMANamedGroup(t *testing.T) {
	re := MustCompile(`\k`, ECMAScript)
	if m, err := re.MatchString("k"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	re = MustCompile(`\k'test'`, ECMAScript)
	if m, err := re.MatchString(`k'test'`); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	re = MustCompile(`\k<test>`, ECMAScript)
	if m, err := re.MatchString(`k<test>`); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	_, err := Compile(`(?<title>\w+), yes \k'title'`, ECMAScript)
	if err == nil {
		t.Fatal("Expected error")
	}

	re = MustCompile(`(?<title>\w+), yes \k<title>`, ECMAScript)
	if m, err := re.MatchString("sir, yes sir"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	re = MustCompile(`\k<title>, yes (?<title>\w+)`, ECMAScript)
	if m, err := re.MatchString(", yes sir"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	_, err = Compile(`\k<(?<name>)>`, ECMAScript)
	if err == nil {
		t.Fatal("Expected error")
	}

	MustCompile(`\k<(<name>)>`, ECMAScript)

	_, err = Compile(`\k<(<name>)>`, 0)
	if err == nil {
		t.Fatal("Expected error")
	}

	re = MustCompile(`\'|\<?`, 0)
	if m, err := re.MatchString("'"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
	if m, err := re.MatchString("<"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestECMAInvalidEscapeCharClass(t *testing.T) {
	re := MustCompile(`[\x0]`, ECMAScript)
	if m, err := re.MatchString("x"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	if m, err := re.MatchString("0"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}

	if m, err := re.MatchString("z"); err != nil {
		t.Fatal(err)
	} else if m {
		t.Fatal("Expected no match")
	}
}

func TestECMAScriptXCurlyBraceEscape(t *testing.T) {
	re := MustCompile(`\x{20}`, ECMAScript)
	if m, err := re.MatchString(" "); err != nil {
		t.Fatal(err)
	} else if m {
		t.Fatal("Expected no match")
	}

	if m, err := re.MatchString("xxxxxxxxxxxxxxxxxxxx"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestEcmaScriptUnicodeRange(t *testing.T) {
	r, err := Compile(`([\u{001a}-\u{ffff}]+)`, ECMAScript|Unicode)
	if err != nil {
		panic(err)
	}
	m, err := r.FindStringMatch("qqqq")
	if err != nil {
		panic(err)
	}
	if m == nil {
		t.Fatal("Expected non-nil, got nil")
	}
}

func TestNegateRange(t *testing.T) {
	re := MustCompile(`[\D]`, 0)
	if m, err := re.MatchString("A"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestECMANegateRange(t *testing.T) {
	re := MustCompile(`[\D]`, ECMAScript)
	if m, err := re.MatchString("A"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestDollar(t *testing.T) {
	// PCRE/C# allow \n to match to $ at end-of-string in singleline mode...
	// a weird edge-case kept for compatibility, ECMAScript/RE2 mode don't allow it
	re := MustCompile(`ac$`, 0)
	if m, err := re.MatchString("ac\n"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}
func TestECMADollar(t *testing.T) {
	re := MustCompile(`ac$`, ECMAScript)
	if m, err := re.MatchString("ac\n"); err != nil {
		t.Fatal(err)
	} else if m {
		t.Fatal("Expected no match")
	}
}

func TestThreeByteUnicode_InputOnly(t *testing.T) {
	// confirm the bmprefix properly ignores 3-byte unicode in the input value
	// this used to panic
	re := MustCompile("高", 0)
	if m, err := re.MatchString("📍Test高"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestMultibyteUnicode_MatchPartialPattern(t *testing.T) {
	re := MustCompile("猟な", 0)
	if m, err := re.MatchString("なあ🍺な"); err != nil {
		t.Fatal(err)
	} else if m {
		t.Fatal("Expected no match")
	}
}

func TestMultibyteUnicode_Match(t *testing.T) {
	re := MustCompile("猟な", 0)
	if m, err := re.MatchString("なあ🍺猟な"); err != nil {
		t.Fatal(err)
	} else if !m {
		t.Fatal("Expected match")
	}
}

func TestAlternationNamedOptions_Errors(t *testing.T) {
	// all of these should give an error "error parsing regexp:"
	data := []string{
		"(?(?e))", "(?(?a)", "(?(?", "(?(", "?(a:b)", "?(a)", "?(a|b)", "?((a)", "?((a)a", "?((a)a|", "?((a)a|b",
		"(?(?i))", "(?(?I))", "(?(?m))", "(?(?M))", "(?(?s))", "(?(?S))", "(?(?x))", "(?(?X))", "(?(?n))", "(?(?N))", " (?(?n))",
	}
	for _, p := range data {
		re, err := Compile(p, 0)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if re != nil {
			t.Fatal("Expected unparsed regexp, got non-nil")
		}

		if !strings.HasPrefix(err.Error(), "error parsing regexp: ") {
			t.Fatalf("Wanted parse error, got '%v'", err)
		}
	}
}

func TestAlternationNamedOptions_Success(t *testing.T) {
	data := []struct {
		pattern       string
		input         string
		expectSuccess bool
		matchVal      string
	}{
		{"(?(cat)|dog)", "cat", true, ""},
		{"(?(cat)|dog)", "catdog", true, ""},
		{"(?(cat)dog1|dog2)", "catdog1", false, ""},
		{"(?(cat)dog1|dog2)", "catdog2", true, "dog2"},
		{"(?(cat)dog1|dog2)", "catdog1dog2", true, "dog2"},
		{"(?(dog2))", "dog2", true, ""},
		{"(?(cat)|dog)", "oof", false, ""},
		{"(?(a:b))", "a", true, ""},
		{"(?(a:))", "a", true, ""},
	}
	for _, p := range data {
		re := MustCompile(p.pattern, 0)
		m, err := re.FindStringMatch(p.input)

		if err != nil {
			t.Fatalf("Unexpected error during match: %v", err)
		}
		if want, got := p.expectSuccess, m != nil; want != got {
			t.Fatalf("Success mismatch for %v, wanted %v, got %v", p.pattern, want, got)
		}
		if m != nil {
			if want, got := p.matchVal, m.String(); want != got {
				t.Fatalf("Match val mismatch for %v, wanted %v, got %v", p.pattern, want, got)
			}
		}
	}
}

func TestAlternationConstruct_Matches(t *testing.T) {
	re := MustCompile("(?(A)A123|C789)", 0)
	m, err := re.FindStringMatch("A123 B456 C789")
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if m == nil {
		t.Fatal("Expected match, got nil")
	}

	if want, got := "A123", m.String(); want != got {
		t.Fatalf("Wanted %v, got %v", want, got)
	}

	m, err = re.FindNextMatch(m)
	if err != nil {
		t.Fatalf("Unexpected err in second match: %v", err)
	}
	if m == nil {
		t.Fatal("Expected second match, got nil")
	}
	if want, got := "C789", m.String(); want != got {
		t.Fatalf("Wanted %v, got %v", want, got)
	}

	m, err = re.FindNextMatch(m)
	if err != nil {
		t.Fatalf("Unexpected err in third match: %v", err)
	}
	if m != nil {
		t.Fatal("Did not expect third match")
	}
}

func TestStartAtEnd(t *testing.T) {
	re := MustCompile("(?:)", 0)
	m, err := re.FindStringMatchStartingAt("t", 1)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("Expected match")
	}
}

func TestParserFuzzCrashes(t *testing.T) {
	var crashes = []string{
		"(?'-", "(\\c0)", "(\\00(?())", "[\\p{0}", "(\x00?.*.()?(()?)?)*.x\xcb?&(\\s\x80)", "\\p{0}", "[0-[\\p{0}",
	}

	for _, c := range crashes {
		t.Log(c)
		Compile(c, 0)
	}
}

func TestParserFuzzHangs(t *testing.T) {
	var hangs = []string{
		"\r{865720113}z\xd5{\r{861o", "\r{915355}\r{9153}", "\r{525005}", "\x01{19765625}", "(\r{068828256})", "\r{677525005}",
	}

	for _, c := range hangs {
		t.Log(c)
		Compile(c, 0)
	}
}

func BenchmarkParserPrefixLongLen(b *testing.B) {
	re := MustCompile("\r{100001}T+", 0)
	inp := strings.Repeat("testing", 10000) + strings.Repeat("\r", 100000) + "TTTT"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(inp); err != nil {
			b.Fatalf("Unexpected err: %v", err)
		} else if m {
			b.Fatalf("Expected no match")
		}
	}
}

/*
func TestPcreStuff(t *testing.T) {
	re := MustCompile(`(?(?=(a))a)`, Debug)
	inp := unEscapeToMatch(`a`)
	fmt.Printf("Inp %q\n", inp)
	m, err := re.FindStringMatch(inp)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if m == nil {
		t.Fatalf("Expected match")
	}

	fmt.Printf("Match %s\n", m.dump())
	fmt.Printf("Text: %v\n", unEscapeGroup(m.String()))

}
*/

//(.*)(\d+) different FirstChars ([\x00-\t\v-\x08] OR [\x00-\t\v-\uffff\p{Nd}]

func TestControlBracketFail(t *testing.T) {
	re := MustCompile(`(cat)(\c[*)(dog)`, 0)
	inp := "asdlkcat\u00FFdogiwod"

	if m, _ := re.MatchString(inp); m {
		t.Fatal("expected no match")
	}
}

func TestControlBracketGroups(t *testing.T) {
	re := MustCompile(`(cat)(\c[*)(dog)`, 0)
	inp := "asdlkcat\u001bdogiwod"

	if want, got := 4, re.capsize; want != got {
		t.Fatalf("Capsize wrong, want %v, got %v", want, got)
	}

	m, _ := re.FindStringMatch(inp)
	if m == nil {
		t.Fatal("expected match")
	}

	g := m.Groups()
	want := []string{"cat\u001bdog", "cat", "\u001b", "dog"}
	for i := 0; i < len(g); i++ {
		if want[i] != g[i].String() {
			t.Fatalf("Bad group num %v, want %v, got %v", i, want[i], g[i].String())
		}
	}
}

func TestBadGroupConstruct(t *testing.T) {
	bad := []string{"(?>-", "(?<", "(?<=", "(?<!", "(?>", "(?)", "(?<)", "(?')", "(?<-"}

	for _, b := range bad {
		_, err := Compile(b, 0)
		if err == nil {
			t.Fatalf("Wanted error, but got no error for pattern: %v", b)
		}
	}
}

func TestEmptyCaptureLargeRepeat(t *testing.T) {
	// a bug would cause our track to not grow and eventually panic
	// with large numbers of repeats of a non-capturing group (>16)

	// the issue was that the jump occured to the same statement over and over
	// and the "grow stack/track" logic only triggered on jumps that moved
	// backwards

	r := MustCompile(`(?:){40}`, 0)
	m, err := r.FindStringMatch("1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if want, got := 0, m.Index; want != got {
		t.Errorf("First Match Index wanted %v got %v", want, got)
	}
	if want, got := 0, m.Length; want != got {
		t.Errorf("First Match Length wanted %v got %v", want, got)
	}

	m, _ = r.FindNextMatch(m)
	if want, got := 1, m.Index; want != got {
		t.Errorf("Second Match Index wanted %v got %v", want, got)
	}
	if want, got := 0, m.Length; want != got {
		t.Errorf("Second Match Length wanted %v got %v", want, got)
	}

	m, _ = r.FindNextMatch(m)
	if m != nil {
		t.Fatal("Expected 2 matches, got more")
	}
}

func TestFuzzBytes_NoCompile(t *testing.T) {
	//some crash cases found from fuzzing

	var testCases = []struct {
		r []byte
	}{
		{
			r: []byte{0x28, 0x28, 0x29, 0x5c, 0x37, 0x28, 0x3f, 0x28, 0x29, 0x29},
		},
		{
			r: []byte{0x28, 0x5c, 0x32, 0x28, 0x3f, 0x28, 0x30, 0x29, 0x29},
		},
		{
			r: []byte{0x28, 0x3f, 0x28, 0x29, 0x29, 0x5c, 0x31, 0x30, 0x28, 0x3f, 0x28, 0x30, 0x29},
		},
		{
			r: []byte{0x28, 0x29, 0x28, 0x28, 0x29, 0x5c, 0x37, 0x28, 0x3f, 0x28, 0x29, 0x29},
		},
	}

	for _, c := range testCases {
		r := string(c.r)
		t.Run(r, func(t *testing.T) {
			_, err := Compile(r, Multiline|ECMAScript|Debug)
			// should fail compiling
			if err == nil {
				t.Fatal("should fail compile, but didn't")
			}
		})
	}

}

func TestFuzzBytes_Match(t *testing.T) {

	var testCases = []struct {
		r, s []byte
	}{
		{
			r: []byte{0x30, 0xbf, 0x30, 0x2a, 0x30, 0x30},
			s: []byte{0xf0, 0xb0, 0x80, 0x91, 0xf7},
		},
		{
			r: []byte{0x30, 0xaf, 0xf3, 0x30, 0x2a},
			s: []byte{0xf3, 0x80, 0x80, 0x87, 0x80, 0x89},
		},
	}

	for _, c := range testCases {
		r := string(c.r)
		t.Run(r, func(t *testing.T) {
			re, err := Compile(r, 0)

			if err != nil {
				t.Fatal("should compile, but didn't")
			}

			re.MatchString(string(c.s))
		})
	}
}

func TestConcatAccidentalPatternCharge(t *testing.T) {
	// originally this pattern would parse incorrectly
	// specifically the closing group would concat the string literals
	// together but the raw rune slice would blow over the original pattern
	// so the final bit of pattern parsing would be wrong
	// fixed in #49
	r, err := Compile(`(?<=1234\.\*56).*(?=890)`, 0)

	if err != nil {
		panic(err)
	}

	m, err := r.FindStringMatch(`1234.*567890`)
	if err != nil {
		panic(err)
	}
	if m == nil {
		t.Fatal("Expected non-nil, got nil")
	}
}

func TestGoodReverseOrderMessage(t *testing.T) {
	_, err := Compile(`[h-c]`, ECMAScript)
	if err == nil {
		t.Fatal("expected error")
	}
	expected := "error parsing regexp: [h-c] range in reverse order in `[h-c]`"
	if err.Error() != expected {
		t.Fatalf("expected %q got %q", expected, err.Error())
	}
}

func TestParseShortSlashP(t *testing.T) {
	re := MustCompile(`[!\pL\pN]{1,}`, 0)
	m, err := re.FindStringMatch("this23! is a! test 1a 2b")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if m.String() != "this23!" {
		t.Fatalf("Expected match")
	}
}

func TestParseShortSlashNegateP(t *testing.T) {
	re := MustCompile(`\PNa`, 0)
	m, err := re.FindStringMatch("this is a test 1a 2b")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if m.String() != " a" {
		t.Fatalf("Expected match")
	}
}

func TestParseShortSlashPEnd(t *testing.T) {
	re := MustCompile(`\pN`, 0)
	m, err := re.FindStringMatch("this is a test 1a 2b")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if m.String() != "1" {
		t.Fatalf("Expected match")
	}
}

func TestMarshal(t *testing.T) {
	re := MustCompile(`.*`, 0)
	m, err := json.Marshal(re)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if string(m) != `".*"` {
		t.Fatalf(`Expected ".*"`)
	}
}

func TestUnMarshal(t *testing.T) {
	DefaultUnmarshalOptions = IgnoreCase
	bytes := []byte(`"^[abc]"`)
	var re *Regexp
	err := json.Unmarshal(bytes, &re)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if re.options != IgnoreCase {
		t.Fatalf("Expected options ignore case")
	}
	if re.String() != `^[abc]` {
		t.Fatalf(`Expected "^[abc]"`)
	}
	ok, err := re.MatchString("A")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf(`Expected match`)
	}
}
