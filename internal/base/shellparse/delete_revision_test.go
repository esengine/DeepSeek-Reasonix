package shellparse

import "testing"

func TestDeleteSequenceAnalysis(t *testing.T) {
	for _, source := range []string{`rm -rf build && make`, `cd app; rm -rf dist 2>/dev/null`} {
		a, err := AnalyzeDeleteCalls(source)
		if err != nil || !a.Standalone {
			t.Fatalf("safe sequence %q = %+v, %v", source, a, err)
		}
	}
}
