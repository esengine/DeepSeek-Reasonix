package builtin

import "testing"

func TestFuzzyFind_ExactMatch(t *testing.T) {
	content := "hello world\nfoo bar\nbaz"
	actual, ok := fuzzyFind(content, "foo bar")
	if !ok {
		t.Fatal("expected exact match")
	}
	if actual != "foo bar" {
		t.Errorf("actual = %q, want %q", actual, "foo bar")
	}
}

func TestFuzzyFind_TrailingWhitespace(t *testing.T) {
	// File has trailing spaces that the model didn't reproduce.
	content := "func main() {   \n\tfmt.Println(\"hello\")  \n}\n"
	old := "func main() {\n\tfmt.Println(\"hello\")\n}"
	actual, ok := fuzzyFind(content, old)
	if !ok {
		t.Fatal("expected trailing-whitespace match")
	}
	// The actual text must come from the file (with trailing spaces).
	if actual != "func main() {   \n\tfmt.Println(\"hello\")  \n}" {
		t.Errorf("actual = %q", actual)
	}
}

func TestFuzzyFind_LeadingWhitespace(t *testing.T) {
	// Model mis-indented by one level (2 spaces instead of 4).
	content := "class Foo:\n    def bar(self):\n        return 42\n"
	old := "  def bar(self):\n    return 42"
	actual, ok := fuzzyFind(content, old)
	if !ok {
		t.Fatal("expected leading-whitespace match")
	}
	// Actual text from the file (with 4-space indent).
	if actual != "    def bar(self):\n        return 42" {
		t.Errorf("actual = %q", actual)
	}
}

func TestFuzzyFind_BothTrailingAndLeading(t *testing.T) {
	content := "    if x > 0:   \n        return x   \n"
	old := "  if x > 0:\n    return x"
	actual, ok := fuzzyFind(content, old)
	if !ok {
		t.Fatal("expected combined whitespace match")
	}
	if actual != "    if x > 0:   \n        return x   " {
		t.Errorf("actual = %q", actual)
	}
}

func TestFuzzyFind_NoMatch(t *testing.T) {
	content := "completely different text\nnothing like the pattern"
	old := "foo bar baz\nqux quux"
	_, ok := fuzzyFind(content, old)
	if ok {
		t.Fatal("expected no match")
	}
}

func TestFuzzyFind_EmptyOld(t *testing.T) {
	_, ok := fuzzyFind("hello", "")
	if ok {
		t.Fatal("empty old should not match")
	}
}

func TestFuzzyFind_SingleLine(t *testing.T) {
	content := "  hello world  "
	old := "hello world"
	actual, ok := fuzzyFind(content, old)
	if !ok {
		t.Fatal("expected single-line whitespace match")
	}
	if actual != "  hello world  " {
		t.Errorf("actual = %q", actual)
	}
}

func TestFuzzyFind_CRLF(t *testing.T) {
	// File uses CRLF, old uses CRLF (as matchLineEndings would convert).
	content := "func main() {   \r\n\treturn nil  \r\n}\r\n"
	old := "func main() {\r\n\treturn nil\r\n}"
	actual, ok := fuzzyFind(content, old)
	if !ok {
		t.Fatal("expected CRLF trailing-whitespace match")
	}
	// The actual text from content includes the \r on each line
	// (since we split on \n, \r stays attached).
	want := "func main() {   \r\n\treturn nil  \r\n}\r"
	if actual != want {
		t.Errorf("actual = %q, want %q", actual, want)
	}
}

func TestFuzzyFind_PreservesOriginalFormatting(t *testing.T) {
	// The replacement should use the actual file text, not the normalized version.
	content := "    line1   \n    line2   \n    line3\n"
	old := "line1\nline2"
	actual, ok := fuzzyFind(content, old)
	if !ok {
		t.Fatal("expected match")
	}
	// Verify actual comes from content so Replace will work.
	if actual != "    line1   \n    line2   " {
		t.Errorf("actual = %q, expected original file text with whitespace", actual)
	}
}
