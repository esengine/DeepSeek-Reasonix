package regexp2

import (
	"strconv"
	"testing"
)

func TestReplace_Basic(t *testing.T) {
	re := MustCompile(`test`, 0)
	str, err := re.Replace("this is a test", "unit", -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "this is a unit", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_NamedGroup(t *testing.T) {
	re := MustCompile(`[^ ]+\s(?<time>)`, 0)
	str, err := re.Replace("08/10/99 16:00", "${time}", -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "16:00", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_IgnoreCaseUpper(t *testing.T) {
	re := MustCompile(`dog`, IgnoreCase)
	str, err := re.Replace("my dog has fleas", "CAT", -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "my CAT has fleas", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_IgnoreCaseLower(t *testing.T) {
	re := MustCompile(`olang`, IgnoreCase)
	str, err := re.Replace("GoLAnG", "olang", -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "Golang", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_NumberGroup(t *testing.T) {
	re := MustCompile(`D\.(.+)`, None)
	str, err := re.Replace("D.Bau", "David $1", -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "David Bau", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_LimitCount(t *testing.T) {
	re := MustCompile(`a`, None)
	str, err := re.Replace("aaaaa", "b", 0, 2)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "bbaaa", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_LimitCountSlice(t *testing.T) {
	re := MustCompile(`a`, None)
	myStr := "aaaaa"
	str, err := re.Replace(myStr, "b", 3, 2)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "aaabb", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_BeginBeforeAfterEnd(t *testing.T) {
	re := MustCompile(`a`, None)
	myStr := "a test a blah and a"
	str, err := re.Replace(myStr, "stuff", -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "stuff test stuff blstuffh stuffnd stuff", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_BadSyntax(t *testing.T) {
	re := MustCompile(`a`, None)
	myStr := "this is a test"
	_, err := re.Replace(myStr, `$5000000000`, -1, -1)
	if err == nil {
		t.Fatalf("Expected err")
	}
}

func TestReplaceFunc_Basic(t *testing.T) {
	re := MustCompile(`test`, None)
	str, err := re.ReplaceFunc("this is a test", func(m Match) string { return "unit" }, -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "this is a unit", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplaceFunc_Multiple(t *testing.T) {
	re := MustCompile(`test`, None)
	count := 0
	str, err := re.ReplaceFunc("This test is another test for stuff", func(m Match) string {
		count++
		return strconv.Itoa(count)
	}, -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "This 1 is another 2 for stuff", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplaceFunc_Groups(t *testing.T) {
	re := MustCompile(`test(?<sub>ing)?`, None)
	count := 0
	str, err := re.ReplaceFunc("This testing is another test testingly junk", func(m Match) string {
		count++
		if m.GroupByName("sub").Length > 0 {
			// we have an "ing", make it negative
			return strconv.Itoa(count * -1)
		}
		return strconv.Itoa(count)
	}, -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "This -1 is another 2 -3ly junk", str; want != got {
		t.Fatalf("Replace failed, wanted %v, got %v", want, got)
	}
}

func TestReplace_RefNumsDollarAmbiguous(t *testing.T) {
	re := MustCompile("(123)hello(789)", None)
	res, err := re.Replace("123hello789", "$1456$2", -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if want, got := "$1456789", res; want != got {
		t.Fatalf("Wrong result: %s", got)
	}
}

func TestReplace_NestedGroups(t *testing.T) {
	re := MustCompile(`(\p{Sc}\s?)?(\d+\.?((?<=\.)\d+)?)(?(1)|\s?\p{Sc})?`, None)
	res, err := re.Replace("$17.43  €2 16.33  £0.98  0.43   £43   12€  17", "$2", -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if want, got := "17.43  2 16.33  0.98  0.43   43   12  17", res; want != got {
		t.Fatalf("Wrong result: %s", got)
	}
}
