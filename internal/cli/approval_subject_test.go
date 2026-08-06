package cli

import (
	"strings"
	"testing"
)

func TestApprovalSubjectBodyShowsClippedCommandInFull(t *testing.T) {
	full := "bash -lc 'cd /very/long/path/to/project && go test ./... -run TestSomethingWithAVeryLongName -count=1 -race'"
	preview := truncateSubject(full, 80)
	if preview == full {
		t.Fatal("fixture must be long enough to clip at this width")
	}

	body := approvalSubjectBody(full, preview, 80)
	if body == "" {
		t.Fatal("a clipped command must be expanded below the banner")
	}
	flattened := strings.ReplaceAll(body, "\n", "")
	if !strings.Contains(flattened, "-race") {
		t.Errorf("the tail of the command must survive wrapping, got %q", body)
	}
}

func TestApprovalSubjectBodyStaysEmptyForShortCommands(t *testing.T) {
	full := "git status"
	if body := approvalSubjectBody(full, full, 80); body != "" {
		t.Errorf("a command the banner already shows must not be repeated, got %q", body)
	}
}

func TestApprovalSubjectBodyIsBounded(t *testing.T) {
	full := strings.Repeat("echo aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa && ", 200)
	body := approvalSubjectBody(full, truncateSubject(full, 60), 60)
	lines := strings.Split(body, "\n")
	if len(lines) > maxApprovalSubjectLines {
		t.Fatalf("expanded command must stay bounded, got %d lines", len(lines))
	}
	if !strings.HasSuffix(lines[len(lines)-1], "…") {
		t.Errorf("a command longer than the bound must end with an ellipsis, got %q", lines[len(lines)-1])
	}
}
