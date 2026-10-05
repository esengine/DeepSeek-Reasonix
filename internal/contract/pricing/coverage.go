package pricing

// Coverage says how much of the usage behind a quote carries a complete price
// fact. CostComplete answers that for one call; an aggregate has two answers
// more, because it can price some of its calls and not others, and it can have
// billed nothing at all. A session that has not spent yet is not a session
// whose price is unknown.
const (
	CoverageNone       = "none"
	CoverageComplete   = "complete"
	CoveragePartial    = "partial"
	CoverageIncomplete = "incomplete"
)

// ValidCoverage reports whether c is one of the four identities.
func ValidCoverage(c string) bool {
	switch c {
	case CoverageNone, CoverageComplete, CoveragePartial, CoverageIncomplete:
		return true
	}
	return false
}

// FoldCoverage returns the coverage of a and b together: the join over complete
// and incomplete, with none as the identity and partial as the top. Order cannot
// reach the answer, and a run that has priced everything so far cannot fold back
// to complete once an unpriced call joins it. A value outside the four folds as
// none; NormalizeQuote repairs a decoded quote before it can reach here.
func FoldCoverage(a, b string) string {
	a, b = coverageOrNone(a), coverageOrNone(b)
	switch {
	case a == CoverageNone:
		return b
	case b == CoverageNone:
		return a
	case a == b:
		return a
	default:
		return CoveragePartial
	}
}

func coverageOrNone(c string) string {
	if ValidCoverage(c) {
		return c
	}
	return CoverageNone
}
