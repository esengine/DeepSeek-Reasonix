package evidence

import (
	"slices"
	"strings"
	"testing"
)

const pytestBase = `import pytest

from calc import median


def helper(xs):
    return sorted(xs)


def test_median_odd():
    """Odd lengths pick the middle."""
    assert median([3, 1, 2]) == 2


@pytest.mark.parametrize("xs,want", [([1, 2], 1.5), ([4], 4)])
def test_median_param(xs, want):
    assert median(xs) == want


class TestEdges:
    def setup_method(self):
        self.empty = []

    def test_empty_raises(self):
        with pytest.raises(ValueError):
            median(self.empty)

    async def test_async(self):
        assert await amedian([1]) == 1


class Helpers:
    def test_not_collected(self):
        assert True


SRC = """
def test_inside_a_string():
    assert False
"""
`

func TestPytestRewrittenCriteria(t *testing.T) {
	cases := []struct {
		name string
		path string
		next string
		want []string
	}{
		{"unchanged", "tests/test_calc.py", pytestBase, nil},
		{"reformatted, recommented, reindented", "tests/test_calc.py", `import pytest
from calc import median
def helper(xs):
    return sorted(xs)
def test_median_odd():
    """A different docstring says nothing about what is asserted."""
    assert median( [3,1,2] )==2  # still two
@pytest.mark.parametrize("xs,want", [
    ([1, 2], 1.5),
    ([4], 4),
])
def test_median_param(xs, want):
    assert median(xs) == want
class TestEdges:
  def setup_method(self):
      self.empty = []
  def test_empty_raises(self):
      with pytest.raises(ValueError):
          median(self.empty)
  async def test_async(self):
      assert await amedian([1]) == 1
class Helpers:
    def test_not_collected(self):
        assert True
SRC = """
def test_inside_a_string():
    assert False
"""
`, nil},
		{"expected value changed", "tests/test_calc.py", replaceOnce(t, pytestBase, "== 2\n", "== 3\n"), []string{"test_median_odd"}},
		{"parametrize case changed", "tests/test_calc.py", replaceOnce(t, pytestBase, "1.5)", "2)"), []string{"test_median_param"}},
		{"method rewritten", "tests/test_calc.py", replaceOnce(t, pytestBase, "ValueError", "Exception"), []string{"TestEdges::test_empty_raises"}},
		{"async method rewritten", "tests/test_calc.py", replaceOnce(t, pytestBase, "== 1\n", "is not None\n"), []string{"TestEdges::test_async"}},
		{"test removed", "tests/test_calc.py", replaceOnce(t, pytestBase, "def test_median_odd():\n    \"\"\"Odd lengths pick the middle.\"\"\"\n    assert median([3, 1, 2]) == 2\n", ""), []string{"test_median_odd"}},
		{"file emptied", "calc_test.py", "", []string{"TestEdges::test_async", "TestEdges::test_empty_raises", "test_median_odd", "test_median_param"}},
		{"test added", "tests/test_calc.py", pytestBase + "\ndef test_new():\n    assert median([1]) == 1\n", nil},
		{"helper and setup are not criteria", "tests/test_calc.py", replaceOnce(t, replaceOnce(t, pytestBase, "sorted(xs)", "list(xs)"), "self.empty = []", "self.empty = ()"), nil},
		{"uncollected class and string contents", "tests/test_calc.py", replaceOnce(t, replaceOnce(t, pytestBase, "assert True", "assert False"), "assert False\n\"\"\"", "assert True\n\"\"\""), nil},
		{"trailing comma in a list", "tests/test_calc.py", replaceOnce(t, pytestBase, "[([1, 2], 1.5), ([4], 4)]", "[([1, 2], 1.5), ([4], 4),]"), nil},
		{"not a pytest module", "calc.py", "", nil},
		{"unterminated string reads as unparsed", "tests/test_calc.py", pytestBase + "x = 'open\n", nil},
		{"unclosed bracket reads as unparsed", "tests/test_calc.py", pytestBase + "x = (1,\n", nil},
		{"inconsistent dedent reads as unparsed", "tests/test_calc.py", pytestBase + "if x:\n        a = 1\n    b = 2\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RewrittenTestCriteria(tc.path, pytestBase, tc.next)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("RewrittenTestCriteria = %q, want %q", got, tc.want)
			}
		})
	}
}

// Detection reads pytest modules; holding them does not, because no backend
// re-runs a captured Python criterion.
func TestPytestModulesAreNotHeld(t *testing.T) {
	if PathMayHoldTestCriteria("tests/test_calc.py") || HoldsTestCriteria("tests/test_calc.py", []byte(pytestBase)) {
		t.Fatal("a pytest module was offered for capture")
	}
}

func TestPyLexerKeepsStringsWhole(t *testing.T) {
	lines, ok := pyLogicalLines("x = rb'a\\'b' + f\"{y}\" + '''multi\nline''' \\\n  + 1\n")
	if !ok || len(lines) != 1 {
		t.Fatalf("lines = %v, ok = %v", lines, ok)
	}
	want := []string{"x", "=", `rb'a\'b'`, "+", `f"{y}"`, "+", "'''multi\nline'''", "+", "1"}
	if !slices.Equal(lines[0].toks, want) {
		t.Fatalf("toks = %q, want %q", lines[0].toks, want)
	}
}

func TestPyDropTrailingCommas(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"(", "x", ",", ")"}, []string{"(", "x", ",", ")"}},
		{[]string{"(", "a", ",", "b", ",", ")"}, []string{"(", "a", ",", "b", ")"}},
		{[]string{"[", "a", ",", "]"}, []string{"[", "a", "]"}},
		{[]string{"f", "(", "(", "x", ",", ")", ",", ")"}, []string{"f", "(", "(", "x", ",", ")", ")"}},
		{[]string{"return", "(", "x", ",", ")"}, []string{"return", "(", "x", ",", ")"}},
		{[]string{"g", "(", ")", "(", "x", ",", ")"}, []string{"g", "(", ")", "(", "x", ")"}},
	}
	for _, tc := range cases {
		if got := pyDropTrailingCommas(tc.in); !slices.Equal(got, tc.want) {
			t.Fatalf("pyDropTrailingCommas(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func replaceOnce(t *testing.T, s, old, repl string) string {
	t.Helper()
	if strings.Count(s, old) != 1 {
		t.Fatalf("%q does not occur exactly once", old)
	}
	return strings.Replace(s, old, repl, 1)
}
