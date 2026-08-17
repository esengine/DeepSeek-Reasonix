package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

func TestRetargetOpenTabsUsesUniqueLinearCompactedLeaf(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(path, id string, turns int, prefix string) {
		t.Helper()
		session := agent.NewSession("sys")
		for i := range turns {
			session.Add(provider.Message{Role: provider.RoleUser, Content: prefix + " question " + strings.Repeat("q", i+1)})
			session.Add(provider.Message{Role: provider.RoleAssistant, Content: prefix + " answer " + strings.Repeat("a", i+1)})
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
		meta := agent.BranchMeta{ID: id, Scope: "global", TopicID: "conversation", TopicTitle: "Compacted"}
		if id != "root" {
			meta.Recovered = true
			meta.ParentID = "root"
			meta.RecoveryDepth = 1
		}
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "root.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	save(root, "root", 9, "root")
	save(leaf, "leaf", 72, "compacted")
	app := NewApp()
	installSessionCatalogForTest(t, app, dir, "global", "")

	topic, ok := app.catalogTopicRecord("global", "", "conversation")
	if !ok || topic.RepresentativePath != leaf {
		t.Fatalf("representative = %q ok=%v, want compacted leaf %q", topic.RepresentativePath, ok, leaf)
	}
	if got := app.catalogSessionPathForTopic("global", "", "conversation"); got != leaf {
		t.Fatalf("catalog session path = %q, want compacted leaf %q", got, leaf)
	}
	if got := sessioncatalog.CanonicalSessionPathForTopic(topic.Sessions, root); got != "" {
		t.Fatalf("content canonical = %q, want unresolved", got)
	}
	idleRoot := &WorkspaceTab{ID: "root", Scope: "global", TopicID: "conversation", SessionPath: root}
	idleLeaf := &WorkspaceTab{ID: "leaf", Scope: "global", TopicID: "conversation", SessionPath: leaf}
	app.tabs = map[string]*WorkspaceTab{"root": idleRoot, "leaf": idleLeaf}

	app.retargetOpenTabsToContinuations()
	if idleRoot.SessionPath != leaf {
		t.Fatalf("root tab path = %q, want compacted leaf %q", idleRoot.SessionPath, leaf)
	}
	if idleLeaf.SessionPath != leaf {
		t.Fatalf("leaf tab regressed to %q, want %q", idleLeaf.SessionPath, leaf)
	}
}
