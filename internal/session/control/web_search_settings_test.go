package control

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/sessionstore"
)

func TestWebSearchModelRefusesRunningTurn(t *testing.T) {
	release := make(chan struct{})
	done := make(chan error, 1)
	c := New(Options{Runner: blockingRunner{session: sessionstore.NewSession("sys"), release: release}})
	defer c.Close()
	go func() { done <- c.RunTurn(context.Background(), "fixture") }()
	waitForRunning(t, c)
	err := c.SaveWebSearchModel("auto")
	close(release)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("turn did not finish")
	}
	if !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("save during turn = %v", err)
	}
}

func TestWebSearchModelSettingPreservesConfig(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := config.UserConfigPath()
	raw := "# user note\nunknown = 42\n[agent]\nweb_search_model = \"lost/model\"\ncustom = \"keep\"\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{})
	defer c.Close()
	if got, err := c.WebSearchModel(); err != nil || got.Stored != "lost/model" {
		t.Fatalf("read = %+v, %v", got, err)
	}
	if err := c.SaveWebSearchModel("missing/model"); !errors.Is(err, config.ErrWebSearchModelUnavailable) {
		t.Fatalf("invalid ref = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != raw {
		t.Fatal("refused edit changed config")
	}
	if err := c.SaveWebSearchModel("auto"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# user note", "unknown = 42", "custom = \"keep\""} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(string(got), "web_search_model") {
		t.Fatalf("auto must clear the key: %s", got)
	}
	if err := c.SaveWebSearchModel(""); err != nil {
		t.Fatal(err)
	}
	if got, err := c.WebSearchModel(); err != nil || got.Stored != "" {
		t.Fatalf("cleared = %+v, %v", got, err)
	}
}

func TestWebSearchModelReportsProjectOverride(t *testing.T) {
	home, root := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(config.UserConfigPath(), []byte("[agent]\nweb_search_model = \"global/model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root+"/reasonix.toml", []byte("[agent]\nweb_search_model = \"project/model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{WorkspaceRoot: root})
	defer c.Close()
	got, err := c.WebSearchModel()
	if err != nil || got.Stored != "global/model" || !got.Overridden || got.Effective != "" || got.Reason != config.WebSearchModelRemoved {
		t.Fatalf("setting = %+v, %v", got, err)
	}
}

func TestWebSearchModelSavesInTheScopeItValidatesIn(t *testing.T) {
	home, root := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	on := true
	user := config.LoadForEdit(config.UserConfigPath())
	user.Providers = []config.ProviderEntry{{Name: "user", Kind: "anthropic", Model: "m", BaseURL: "https://user.invalid", WebSearch: &on}}
	if err := user.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	project := "[[providers]]\nname = \"projonly\"\nkind = \"anthropic\"\nmodel = \"p\"\nbase_url = \"https://proj.invalid\"\nweb_search = true\napi_key = \"x\"\n"
	if err := os.WriteFile(root+"/reasonix.toml", []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{WorkspaceRoot: root})
	defer c.Close()
	if err := c.SaveWebSearchModel("projonly/p"); !errors.Is(err, config.ErrWebSearchModelUnavailable) {
		t.Fatalf("a project-only provider became a global selection: %v", err)
	}
	if got, _ := os.ReadFile(config.UserConfigPath()); strings.Contains(string(got), "projonly") {
		t.Fatal("global config now names a project-only provider")
	}
}
