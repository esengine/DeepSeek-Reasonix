package inspect

import (
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/config"
)

func TestProviders(t *testing.T) {
	cfg := config.Default()
	got := Providers(cfg)
	if len(got) == 0 {
		t.Fatal("expected providers from default config")
	}
	// The default provider "deepseek" should be marked as default.
	found := false
	for _, p := range got {
		if p.Name == "deepseek" && p.IsDefault {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected deepseek provider to be marked as default")
	}
	if Providers(nil) != nil {
		t.Error("nil config should project to nil")
	}
}

func TestCommands(t *testing.T) {
	cmds := []command.Command{
		{Name: "review", Description: "Review the diff", Source: "/x/review.md"},
		{Name: "git:commit", Description: "Commit", Source: "/x/git/commit.md"},
	}
	got := Commands(cmds)
	if len(got) != 2 || got[0].Name != "review" {
		t.Fatalf("command projection wrong: %+v", got)
	}
	if Commands(nil) != nil {
		t.Error("nil commands should project to nil")
	}
}

// TestNilInputsSafe ensures every projector tolerates absent runtime objects —
// a desktop front-end may query capabilities before plugins/registry exist.
func TestNilInputsSafe(t *testing.T) {
	if Providers(nil) != nil || Tools(nil) != nil ||
		Servers(nil) != nil || Prompts(nil) != nil || Resources(nil) != nil {
		t.Error("nil inputs should project to nil slices")
	}
	snap := Capabilities(nil, nil, nil, nil)
	if snap.DefaultModel != "" || snap.Providers != nil || snap.Tools != nil {
		t.Errorf("empty snapshot expected, got %+v", snap)
	}
}
