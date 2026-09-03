package agent

import (
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/sessioncontext"
)

func TestCompactionExcludesAllContextsFromSummaryAndKeepsLatestFoldSnapshot(t *testing.T) {
	old := HostGeneratedUserMessage(sessioncontext.Build(sessioncontext.Sections{Workspace: "old"}).Content)
	latestSnapshot := sessioncontext.Build(sessioncontext.Sections{Workspace: "new", SkillsCatalog: "catalog"})
	latest := HostGeneratedUserMessage(latestSnapshot.Content)
	region := []provider.Message{
		old,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "first request"},
		{Role: provider.RoleAssistant, Content: "first answer"},
		latest,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "second request"},
		{Role: provider.RoleAssistant, Content: "second answer"},
	}
	a := &Agent{}
	kept, fold, retention := a.partitionFoldForProjectionAt(region, 1, 1+latestSessionContextIndex(region))
	if len(kept) != 1 || kept[0].Content != latestSnapshot.Content || kept[0].Origin != provider.MessageOriginHost {
		t.Fatalf("kept context = %+v", kept)
	}
	if len(fold) != 4 || retention.Dropped != 2 {
		t.Fatalf("fold=%+v retention=%+v", fold, retention)
	}
	for _, message := range fold {
		if isSessionContextMessage(message) {
			t.Fatalf("summarizer input retained session context: %+v", message)
		}
	}

	projection := checkpointProjectionMessages(
		append([]provider.Message{{Role: provider.RoleSystem, Content: "stable"}}, region...),
		1,
		kept,
		"conversation summary",
	)
	if len(projection) != 3 || projection[0].Role != provider.RoleSystem ||
		projection[1].Content != latestSnapshot.Content || projection[1].Origin != provider.MessageOriginHost ||
		!isCompactionSummary(projection[2]) {
		t.Fatalf("checkpoint projection order = %+v", projection)
	}
	model := provider.ModelMessages(projection)
	if model[1].Origin != "" || model[1].Content != latestSnapshot.Content {
		t.Fatalf("provider boundary changed context bytes or kept provenance: %+v", model[1])
	}
}

func TestCompactionDropsFoldContextsWhenLatestRemainsInRecentTail(t *testing.T) {
	old := HostGeneratedUserMessage(sessioncontext.Build(sessioncontext.Sections{Workspace: "old"}).Content)
	latest := HostGeneratedUserMessage(sessioncontext.Build(sessioncontext.Sections{Workspace: "tail"}).Content)
	all := []provider.Message{
		{Role: provider.RoleSystem, Content: "stable"}, old,
		{Role: provider.RoleUser, Content: "fold me"}, {Role: provider.RoleAssistant, Content: "answer"},
		latest, {Role: provider.RoleUser, Content: "recent"},
	}
	a := &Agent{}
	kept, fold, _ := a.partitionFoldForProjectionAt(all[1:4], 1, latestSessionContextIndex(all))
	if len(kept) != 0 || len(fold) != 2 {
		t.Fatalf("kept=%+v fold=%+v", kept, fold)
	}
	if all[4].Content != latest.Content {
		t.Fatal("recent-tail context bytes changed")
	}
}

func TestExplicitCompressionExcludesContextsAndDropsOnlySelectedOldSnapshots(t *testing.T) {
	old := HostGeneratedUserMessage(sessioncontext.Build(sessioncontext.Sections{Workspace: "old"}).Content)
	latest := HostGeneratedUserMessage(sessioncontext.Build(sessioncontext.Sections{Workspace: "latest"}).Content)
	visible := []provider.Message{
		{Role: provider.RoleSystem, Content: "stable"}, old,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "first request"},
		{Role: provider.RoleAssistant, Content: "first answer"}, latest,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "second request"},
	}
	a := &Agent{}
	plan, ok := a.planVisibleCompression(explicitCompressionSnapshot{visible: visible}, "before", 5, "second")
	if !ok {
		t.Fatalf("plan = %+v", plan)
	}
	for _, message := range plan.fold {
		if isSessionContextMessage(message) {
			t.Fatalf("explicit summarizer retained context: %+v", message)
		}
	}
	projection := buildVisibleCompressionProjection(visible, plan, "summary")
	if got := countSessionContexts(projection); got != 1 {
		t.Fatalf("projection context count = %d, want latest only: %+v", got, projection)
	}
	if snapshot, ok := latestTurnContextSnapshot(projection); !ok || snapshot.Content != latest.Content {
		t.Fatalf("projection latest context = %+v, %v", snapshot, ok)
	}
}
