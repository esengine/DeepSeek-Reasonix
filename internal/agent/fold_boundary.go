package agent

// A fold reads the working view — frozen projection body, then
// canonical[priorCovered:] — but records its boundary canonically. The two
// systems agree only past the body: its digest stands for a range no index names.

// foldBoundary is one fold's landing point once expressed canonically.
type foldBoundary struct {
	// Covered is the canonical prefix the new projection claims. It never moves
	// backwards: history a digest already folded stays folded.
	Covered int
	// BodySuffixFrom indexes the frozen body remainder the new projection must
	// carry forward, as body[BodySuffixFrom:]. It equals the body length when
	// nothing survives, so an empty suffix needs no second field to say so.
	BodySuffixFrom int
}

// mapFoldBoundary translates a working-view boundary into canonical
// coordinates. projected decides: false means the view is the transcript and
// bodyLen/priorCovered are ignored, so a stale pair cannot contradict it. At or
// inside the body, coverage stays at priorCovered — those indices name no
// canonical message — and the remainder the digest left behind stays frozen.
func mapFoldBoundary(start, bodyLen, priorCovered int, projected bool) foldBoundary {
	start = max(start, 0)
	if !projected {
		return foldBoundary{Covered: start}
	}
	bodyLen = max(bodyLen, 0)
	priorCovered = max(priorCovered, 0)
	if start < bodyLen {
		return foldBoundary{Covered: priorCovered, BodySuffixFrom: start}
	}
	return foldBoundary{Covered: priorCovered + (start - bodyLen), BodySuffixFrom: bodyLen}
}
