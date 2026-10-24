package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/runtime/isolation"
)

type rootCommands struct {
	commandResults
	request isolation.RootSceneRequest
}

func (f *rootCommands) ManageRootScene(_ context.Context, request isolation.RootSceneRequest) error {
	f.request = request
	return f.refused
}

func TestRootSceneCommandsPreservePublicationAndRecoveryPreview(t *testing.T) {
	row := &isolation.RootSceneSummary{EnvironmentID: "env", Generation: "gen", Runtime: "runtime", Owner: "session", PublicationID: "publication", SourceWorkspace: "/source", LiveWorkspace: "/live", State: "pending", Actions: []string{"retry", "recheck", "recover"}, Recovery: &isolation.RecoveryTarget{Workspace: "/source", Revision: "revision", Paths: []string{"/source/file"}}}
	f := &rootCommands{commandResults: commandResults{catalog: &isolation.Catalog{Enabled: true, RootScene: row}}}
	text, err := IsolationCommandText(t.Context(), f, "list")
	if err != nil || !strings.Contains(text, "/source") || !strings.Contains(text, "/live") {
		t.Fatalf("list lost scene identities: %s %v", text, err)
	}
	for _, action := range []string{"recheck", "retry"} {
		if _, err := IsolationCommandText(t.Context(), f, "root "+action); err != nil {
			t.Fatal(err)
		}
		if f.request.EnvironmentID != "env" || f.request.Runtime != "runtime" || f.request.Owner != "session" || f.request.PublicationID != "publication" || f.request.Action != action {
			t.Fatalf("request changed: %+v", f.request)
		}
	}
	f.request = isolation.RootSceneRequest{}
	text, err = IsolationCommandText(t.Context(), f, "root recover")
	if err != nil || f.request.Action != "" || !strings.Contains(text, "/source/file") {
		t.Fatalf("preview executed recovery: %s %v", text, err)
	}
	command := strings.TrimPrefix(text[strings.LastIndex(text, "\n")+1:], "/isolated ")
	if _, err := IsolationCommandText(t.Context(), f, command); err != nil {
		t.Fatal(err)
	}
	if f.request.Revision != "revision" || f.request.Action != "recover" {
		t.Fatalf("recovery binding lost: %+v", f.request)
	}
	row.Actions = []string{}
	if _, err := IsolationCommandText(t.Context(), f, "root retry"); err == nil {
		t.Fatal("unavailable root action accepted")
	}
}
