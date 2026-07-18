package runtimeservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/sessiondisplay"
)

func promptHistoryTestService(t *testing.T) *PromptHistoryService {
	t.Helper()
	service, err := NewPromptHistoryService(PromptHistoryOptions{CursorKey: []byte("deterministic prompt history test key")})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func promptHistorySource(workspace, session, dir, path string) PromptHistorySessionSource {
	return PromptHistorySessionSource{
		Session:    runtimeapi.SessionRef{WorkspaceID: runtimeapi.WorkspaceID(workspace), SessionID: runtimeapi.SessionID(session)},
		SessionDir: dir, SessionPath: path,
	}
}

func TestPromptHistoryScansLegacyCurrentEventLogDisplayAndSynthetic(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy.jsonl")
	legacy := `{"kind":"user.message","text":"legacy prompt","time":1800000000123}` + "\n" +
		`{"type":"user.message","text":"Plan approved — plan mode is off"}` + "\n"
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	currentPath := filepath.Join(dir, "current.jsonl")
	expanded := "<memory-update>\nremember\n</memory-update>\n\nvisible prompt"
	current := fmt.Sprintf("%s\n%s\n",
		`{"role":"user","content":"modern prompt","createdAt":"2027-01-15T08:00:00Z"}`,
		fmt.Sprintf(`{"role":"user","content":%q,"timestamp":1800000001123}`, expanded),
	)
	if err := os.WriteFile(currentPath, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sessiondisplay.Record(dir, currentPath, expanded, "visible prompt"); err != nil {
		t.Fatal(err)
	}

	eventPath := filepath.Join(dir, "event.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "event first"})
	if err := session.SaveSnapshot(eventPath); err != nil {
		t.Fatal(err)
	}
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	session.Add(provider.Message{Role: provider.RoleUser, Content: "event second"})
	if err := session.SaveSnapshot(eventPath); err != nil {
		t.Fatal(err)
	}
	if !agent.HasNativeSessionEventLog(eventPath) {
		t.Fatal("test did not create a native event log")
	}

	sources := []PromptHistorySessionSource{
		promptHistorySource("workspace-a", "session-legacy", dir, legacyPath),
		promptHistorySource("workspace-a", "session-current", dir, currentPath),
		promptHistorySource("workspace-a", "session-event", dir, eventPath),
	}
	page, err := promptHistoryTestService(t).History(context.Background(), runtimeapi.PromptHistoryInput{WorkspaceID: "workspace-a"}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 5 || page.HasMore || page.Next != "" {
		t.Fatalf("history page = %+v, want five complete entries", page)
	}
	byText := make(map[string]runtimeapi.PromptHistoryEntry, len(page.Entries))
	for _, entry := range page.Entries {
		byText[entry.Text] = entry
		if strings.Contains(entry.Text, "memory-update") || strings.Contains(entry.Text, "Plan approved") {
			t.Fatalf("unprojected or synthetic prompt crossed history: %+v", entry)
		}
	}
	for _, text := range []string{"legacy prompt", "modern prompt", "visible prompt", "event first", "event second"} {
		if _, ok := byText[text]; !ok {
			t.Fatalf("history missing %q: %+v", text, page.Entries)
		}
	}
	if byText["legacy prompt"].AtMillis != 1800000000123 || byText["visible prompt"].AtMillis != 1800000001123 {
		t.Fatalf("event timestamps were not preserved: %+v", byText)
	}
	if byText["event first"].Session.SessionID != "session-event" || byText["event second"].Turn != 1 {
		t.Fatalf("event-log target/turn projection = %+v", byText)
	}
}

func TestPromptHistoryCursorIsStablePagedAndExpiresOnRevisionChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	body := `{"role":"user","content":"one","time":1800000000001}` + "\n" +
		`{"role":"user","content":"two","time":1800000000002}` + "\n" +
		`{"role":"user","content":"three","time":1800000000003}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	service := promptHistoryTestService(t)
	sources := []PromptHistorySessionSource{promptHistorySource("workspace-a", "session-a", dir, path)}
	input := runtimeapi.PromptHistoryInput{WorkspaceID: "workspace-a", Limit: 2}
	first, err := service.History(context.Background(), input, sources)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := service.History(context.Background(), input, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 2 || first.Entries[0].Text != "three" || first.Entries[1].Text != "two" ||
		!first.HasMore || first.Next == "" || repeated.Next != first.Next {
		t.Fatalf("first/repeated pages = %+v / %+v", first, repeated)
	}
	input.Cursor = first.Next
	second, err := service.History(context.Background(), input, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 1 || second.Entries[0].Text != "one" || second.HasMore || second.Next != "" {
		t.Fatalf("second page = %+v", second)
	}
	if err := os.WriteFile(path, []byte(body+`{"role":"user","content":"four","time":1800000000004}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.History(context.Background(), input, sources); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("changed transcript cursor error = %v, want ErrStaleCursor", err)
	}
	input.Cursor = "not-an-opaque-cursor"
	if _, err := service.History(context.Background(), input, sources); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("malformed cursor error = %v, want ErrInvalidCursor", err)
	}
}

func TestPromptHistoryUsesSharedDefaultAndMaximumPageRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "many.jsonl")
	var body strings.Builder
	for index := 0; index < runtimeapi.PageDefaultItems+1; index++ {
		fmt.Fprintf(&body, "{\"role\":\"user\",\"content\":\"prompt-%03d\"}\n", index)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	service := promptHistoryTestService(t)
	sources := []PromptHistorySessionSource{promptHistorySource("workspace-a", "session-many", dir, path)}
	page, err := service.History(context.Background(), runtimeapi.PromptHistoryInput{WorkspaceID: "workspace-a"}, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != runtimeapi.PageDefaultItems || !page.HasMore || page.Next == "" {
		t.Fatalf("default page len/more/cursor = %d/%v/%q", len(page.Entries), page.HasMore, page.Next)
	}
	_, err = service.History(context.Background(), runtimeapi.PromptHistoryInput{
		WorkspaceID: "workspace-a", Limit: runtimeapi.PageMaxItems + 1,
	}, sources)
	if !errors.Is(err, ErrQueryFailed) {
		t.Fatalf("over-limit error = %v, want ErrQueryFailed", err)
	}
}

func TestPromptHistoryRejectsCrossWorkspaceAndEscapingSource(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, []byte(`{"role":"user","content":"secret"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := promptHistoryTestService(t)
	_, err := service.History(context.Background(), runtimeapi.PromptHistoryInput{WorkspaceID: "workspace-a"}, []PromptHistorySessionSource{
		promptHistorySource("workspace-b", "session-b", dir, outside),
	})
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("cross-workspace source error = %v", err)
	}
	_, err = service.History(context.Background(), runtimeapi.PromptHistoryInput{WorkspaceID: "workspace-a"}, []PromptHistorySessionSource{
		promptHistorySource("workspace-a", "session-a", dir, outside),
	})
	if !errors.Is(err, ErrQueryFailed) {
		t.Fatalf("escaping source error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.History(ctx, runtimeapi.PromptHistoryInput{WorkspaceID: "workspace-a"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled history error = %v", err)
	}
}

func TestParsePromptHistoryMillisNormalizesSupportedUnits(t *testing.T) {
	base := time.Date(2027, 1, 15, 8, 0, 0, 123_000_000, time.UTC)
	for _, raw := range []string{
		fmt.Sprintf("%d", base.Unix()), fmt.Sprintf("%d", base.UnixMilli()),
		fmt.Sprintf("%d", base.UnixMicro()), fmt.Sprintf("%d", base.UnixNano()),
		fmt.Sprintf("%q", base.Format(time.RFC3339Nano)),
	} {
		value, ok := parsePromptHistoryMillis([]byte(raw))
		if !ok || value/1000 != base.Unix() {
			t.Fatalf("parse %s = %d, %v", raw, value, ok)
		}
	}
}
