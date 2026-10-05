package evidence

import "testing"

const criteriaBefore = `package m

import "testing"

func TestMedianOdd(t *testing.T) {
	if got := Median([]int{3, 1, 2}); got != 2 {
		t.Fatalf("Median = %v, want 2", got)
	}
}

func TestMean(t *testing.T) {
	if got := Mean([]int{1, 2}); got != 1.5 {
		t.Fatalf("Mean = %v", got)
	}
}
`

// The observed failure mode: an impossible requirement is "met" by moving the
// check. Both shapes seen in real runs are here — the expectation rewritten,
// and the input rewritten so the old expectation still holds.
func TestRewrittenExpectationAndInputAreBothCriteriaChanges(t *testing.T) {
	expectation := `package m

import "testing"

func TestMedianOdd(t *testing.T) {
	if got := Median([]int{3, 1, 2}); got != 1 {
		t.Fatalf("Median = %v, want 1", got)
	}
}

func TestMean(t *testing.T) {
	if got := Mean([]int{1, 2}); got != 1.5 {
		t.Fatalf("Mean = %v", got)
	}
}
`
	got := RewrittenTestCriteria("mathutil/mathutil_test.go", criteriaBefore, expectation)
	if len(got) != 1 || got[0] != "TestMedianOdd" {
		t.Fatalf("rewritten expectation = %v, want [TestMedianOdd]", got)
	}

	input := `package m

import "testing"

func TestMedianOdd(t *testing.T) {
	if got := Median([]int{1, 2, 3}); got != 2 {
		t.Fatalf("Median = %v, want 2", got)
	}
}

func TestMean(t *testing.T) {
	if got := Mean([]int{1, 2}); got != 1.5 {
		t.Fatalf("Mean = %v", got)
	}
}
`
	if got := RewrittenTestCriteria("mathutil/mathutil_test.go", criteriaBefore, input); len(got) != 1 || got[0] != "TestMedianOdd" {
		t.Fatalf("rewritten input = %v, want [TestMedianOdd]", got)
	}
}

// Adding a test is how a fix is supposed to land, and reformatting is not a
// change of meaning. Neither may be reported, or the signal stops being read.
func TestAddedTestAndReformattingAreNotCriteriaChanges(t *testing.T) {
	added := criteriaBefore + `
func TestMedianEven(t *testing.T) {
	if got := Median([]int{1, 2, 3, 4}); got != 2.5 {
		t.Fatalf("Median = %v, want 2.5", got)
	}
}
`
	if got := RewrittenTestCriteria("m_test.go", criteriaBefore, added); len(got) != 0 {
		t.Fatalf("adding a test reported %v, want none", got)
	}

	reformatted := `package m

import "testing"

func TestMedianOdd(t *testing.T) {
	if got := Median([]int{3, 1, 2}); got != 2 {

		t.Fatalf("Median = %v, want 2", got)
	}
}

func TestMean(t *testing.T) { if got := Mean([]int{1, 2}); got != 1.5 { t.Fatalf("Mean = %v", got) } }
`
	if got := RewrittenTestCriteria("m_test.go", criteriaBefore, reformatted); len(got) != 0 {
		t.Fatalf("reformatting reported %v, want none", got)
	}
}

// Removing a test removes the measure just as surely as rewriting it.
func TestRemovedTestIsACriteriaChange(t *testing.T) {
	removed := `package m

import "testing"

func TestMean(t *testing.T) {
	if got := Mean([]int{1, 2}); got != 1.5 {
		t.Fatalf("Mean = %v", got)
	}
}
`
	if got := RewrittenTestCriteria("m_test.go", criteriaBefore, removed); len(got) != 1 || got[0] != "TestMedianOdd" {
		t.Fatalf("removed test = %v, want [TestMedianOdd]", got)
	}
}

// Non-Go tests and unparseable edits yield nothing rather than a guess.
func TestUnreadableEditsReportNothing(t *testing.T) {
	if got := RewrittenTestCriteria("src/app.test.ts", "it('a', () => expect(1).toBe(1))", "it('a', () => expect(1).toBe(2))"); got != nil {
		t.Fatalf("TypeScript edit = %v, want nil", got)
	}
	if got := RewrittenTestCriteria("m_test.go", criteriaBefore, "package m\nfunc broken( {"); got != nil {
		t.Fatalf("unparseable edit = %v, want nil", got)
	}
}
