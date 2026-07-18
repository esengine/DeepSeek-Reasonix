package autoresearch

import (
	"strings"
	"testing"
	"time"
)

func TestRemoteBoundedReadsRejectOverflowWithoutChangingLocalReads(t *testing.T) {
	store := NewStore(t.TempDir())
	task, err := store.CreateTask("remote bounded reads", CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendFinding(task.ID, Finding{
		ID: "large-finding", Kind: FindingKindManual, Summary: strings.Repeat("x", 4096),
		Source: FindingSourceManual, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.FindingsBounded(task.ID, 10, 128); err == nil {
		t.Fatal("FindingsBounded accepted a source tail above its byte budget")
	}
	if findings, err := store.Findings(task.ID, 10); err != nil || len(findings) != 1 {
		t.Fatalf("historical local Findings changed: len=%d err=%v", len(findings), err)
	}
	if _, err := store.SummaryBounded(task.ID, 128); err == nil {
		t.Fatal("SummaryBounded accepted task state above its byte budget")
	}
	if summary, err := store.Summary(task.ID); err != nil || summary.FindingCount != 1 {
		t.Fatalf("historical local Summary changed: summary=%+v err=%v", summary, err)
	}
}

func TestListSummariesLimitRejectsPossiblyPartialDirectoryBoundary(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.CreateTask("bounded list", CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListSummariesLimit(1); err == nil {
		t.Fatal("ListSummariesLimit accepted an ambiguous directory boundary")
	}
	if summaries, err := store.ListSummariesLimit(2); err != nil || len(summaries) != 1 {
		t.Fatalf("ListSummariesLimit(2): len=%d err=%v", len(summaries), err)
	}
}
