package sessioncatalog

import (
	"context"
	"testing"
)

func TestListTopicsManualOrderIsAppliedBeforePagination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	activity := map[string]int64{"a": 300, "b": 200, "c": 100, "unranked": 400}
	for topicID, lastActivity := range activity {
		if err := catalog.UpsertSession(ctx, SessionRecord{
			Path: "/sessions/" + topicID + ".jsonl", Directory: "/sessions",
			Scope: "global", TopicID: topicID, TopicTitle: topicID,
			LastActivityAt: lastActivity, Turns: 1, TurnsState: TurnsValid, Health: HealthOK,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := catalog.SyncMetadata(ctx, nil, []TopicMetadata{
		{Scope: "global", TopicID: "a", Title: "a", SortOrder: 2},
		{Scope: "global", TopicID: "b", Title: "b", SortOrder: 1},
		{Scope: "global", TopicID: "c", Title: "c", SortOrder: 0},
	}); err != nil {
		t.Fatal(err)
	}

	first, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 2, ManualOrder: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := catalog.ListTopics(ctx, TopicPageRequest{
		Scope: "global", Limit: 2, ManualOrder: true, Cursor: first.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || len(second.Items) != 2 || first.NextCursor == "" || second.NextCursor != "" {
		t.Fatalf("manual pages = first %#v second %#v", first, second)
	}
	got := []string{first.Items[0].TopicID, first.Items[1].TopicID, second.Items[0].TopicID, second.Items[1].TopicID}
	want := []string{"c", "b", "a", "unranked"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("manual keyset order = %v, want %v", got, want)
		}
	}
	if second.Items[1].SortOrder != -1 {
		t.Fatalf("unranked sort order = %d, want -1", second.Items[1].SortOrder)
	}
	if _, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 2, Cursor: first.NextCursor}); err == nil {
		t.Fatal("manual cursor reused with activity ordering should fail")
	}
}
