package sessioncatalog

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSyncMetadataKeepsTopicTitleIndependentFromSessionNote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: "/sessions/titled.jsonl", Directory: "/sessions", Scope: "global",
		TopicID: "titled", TopicTitle: "Original topic", CustomTitle: "Explicit session title",
		LastActivityAt: 2, Turns: 1, TurnsState: TurnsValid, Health: HealthOK,
	}
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SyncMetadata(ctx, nil, []TopicMetadata{{
		Scope: "global", TopicID: "titled", Title: "Changed topic", TitleSource: "auto",
	}}); err != nil {
		t.Fatal(err)
	}
	topic, ok, err := catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "titled"})
	if err != nil || !ok || topic.Title != "Changed topic" || topic.TitleSource != "auto" {
		t.Fatalf("topic title after metadata sync = %+v, ok=%v, err=%v", topic, ok, err)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Changed topic" || page.Items[0].TitleSource != "auto" ||
		len(page.Items[0].Sessions) != 1 || page.Items[0].Sessions[0].CustomTitle != "Explicit session title" {
		t.Fatalf("listed topic/session titles = %+v, err=%v", page.Items, err)
	}
	record.CustomTitle = ""
	record.TopicTitle = "Session-side topic title"
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SyncMetadata(ctx, nil, []TopicMetadata{{
		Scope: "global", TopicID: "titled", Title: "Renamed topic", TitleSource: "manual",
	}}); err != nil {
		t.Fatal(err)
	}
	topic, ok, err = catalog.GetTopic(ctx, TopicKey{Scope: "global", TopicID: "titled"})
	if err != nil || !ok || topic.Title != "Renamed topic" || topic.TitleSource != "manual" ||
		len(topic.Sessions) != 1 || topic.Sessions[0].CustomTitle != "" {
		t.Fatalf("independent cleared session note = %+v, ok=%v, err=%v", topic, ok, err)
	}
}
