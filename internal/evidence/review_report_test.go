package evidence

import "testing"

func TestReviewReportCoverageRejectsSameBasenameInAnotherDirectory(t *testing.T) {
	report := ReviewReport{ReviewedPaths: []string{"other/agent.go"}}
	if report.CoversPaths([]string{"internal/agent/agent.go"}) {
		t.Fatal("same basename in another directory must not cover the required path")
	}
	report.ReviewedPaths = []string{"/workspace/internal/agent/agent.go"}
	if !report.CoversPaths([]string{"internal/agent/agent.go"}) {
		t.Fatal("a fuller absolute path must cover the same relative target")
	}
	report.ReviewedPaths = []string{"agent.go"}
	if report.CoversPaths([]string{"internal/agent/agent.go"}) {
		t.Fatal("a bare basename must not cover a directory-qualified target")
	}
}
