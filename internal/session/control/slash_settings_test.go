package control

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func settingsController(t *testing.T) (*Controller, func() []string) {
	t.Helper()
	prev := i18n.CurrentLanguage()
	t.Cleanup(func() { i18n.DetectLanguage(prev) })
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})
	c := New(Options{Sink: sink, Registry: tool.NewRegistry(), WorkspaceRoot: testenv.TempDir(t)})
	return c, func() []string { out := notices; notices = nil; return out }
}

func lastNotice(t *testing.T, got []string) string {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("want exactly one notice, got %d: %q", len(got), got)
	}
	return got[0]
}

func seedUserConfig(t *testing.T, body string) string {
	t.Helper()
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSettingsVerbsAreHandledOnSubmitPath(t *testing.T) {
	c, take := settingsController(t)
	for _, line := range []string{"/sandbox", "/output-style", "/output-styles", "/forget", "/rename", "/language", "/currency", "/reasoning-language", "/effort"} {
		if !c.managementNotice(line) {
			t.Errorf("%s was not handled", line)
		}
	}
	take()
	if c.managementNotice("/effort high") {
		t.Error("/effort <level> is the frontend's switch, not a notice")
	}
}

func TestSandboxStatusNamesBothPhases(t *testing.T) {
	c, take := settingsController(t)
	c.managementNotice("/sandbox")
	got := lastNotice(t, take())
	for _, want := range []string{"phase 0", "phase 1", "bash", "network"} {
		if !strings.Contains(got, want) {
			t.Errorf("sandbox status missing %q: %s", want, got)
		}
	}
}

func TestOutputStyleListsBuiltins(t *testing.T) {
	c, take := settingsController(t)
	c.managementNotice("/output-style")
	got := lastNotice(t, take())
	if !strings.Contains(got, "explanatory") || !strings.Contains(got, "(builtin)") {
		t.Errorf("output styles = %q", got)
	}
}

func TestReasoningLanguagePersistsAndKeepsUnknownKeys(t *testing.T) {
	c, take := settingsController(t)
	path := seedUserConfig(t, "[future_section]\nflag = \"keep-me\"\n")
	c.managementNotice("/reasoning-language")
	if got := lastNotice(t, take()); !strings.Contains(got, "reasoning-language: auto") {
		t.Errorf("show = %q", got)
	}
	c.managementNotice("/reasoning-language zh")
	if got := lastNotice(t, take()); got != "reasoning-language set to zh" {
		t.Errorf("set = %q", got)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "reasoning_language") || !strings.Contains(string(raw), "keep-me") {
		t.Errorf("config after set:\n%s", raw)
	}
	c.managementNotice("/reasoning-language klingon")
	if got := lastNotice(t, take()); !strings.Contains(got, "must be auto|zh|en") {
		t.Errorf("bad value = %q", got)
	}
}

func TestCurrencyPersists(t *testing.T) {
	c, take := settingsController(t)
	path := seedUserConfig(t, "[future_section]\nflag = \"keep-me\"\n")
	c.managementNotice("/currency")
	if got := lastNotice(t, take()); !strings.Contains(got, "auto") || !strings.Contains(got, "USD") {
		t.Errorf("show = %q", got)
	}
	c.managementNotice("/currency usd")
	if got := lastNotice(t, take()); !strings.Contains(got, "USD") {
		t.Errorf("set = %q", got)
	}
	cfg, err := config.LoadForEditReadOnlyStrict(path)
	if err != nil || cfg.DisplayCurrencyPref() != "USD" {
		t.Errorf("stored pref = %v, %v", cfg, err)
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), "keep-me") {
		t.Errorf("unknown key lost:\n%s", raw)
	}
	c.managementNotice("/currency gbp")
	if got := lastNotice(t, take()); !strings.Contains(got, "must be auto|CNY|USD") {
		t.Errorf("bad value = %q", got)
	}
}

func TestLanguagePersists(t *testing.T) {
	c, take := settingsController(t)
	path := seedUserConfig(t, "[future_section]\nflag = \"keep-me\"\n")
	c.managementNotice("/language")
	if got := lastNotice(t, take()); !strings.Contains(got, "auto") || !strings.Contains(got, "zh") {
		t.Errorf("show = %q", got)
	}
	c.managementNotice("/language zh")
	if got := lastNotice(t, take()); got != fmt.Sprintf(i18n.M.LanguageChangedFmt, "zh", "zh") {
		t.Errorf("set = %q", got)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"zh"`) || !strings.Contains(string(raw), "keep-me") {
		t.Errorf("config after set:\n%s", raw)
	}
	c.managementNotice("/language fr")
	if got := lastNotice(t, take()); got != "usage: /language auto|en|zh" {
		t.Errorf("bad value = %q", got)
	}
}

func TestForgetUsageAndUnknownName(t *testing.T) {
	c, take := settingsController(t)
	c.managementNotice("/forget")
	if got := lastNotice(t, take()); got != i18n.M.ForgetUsage {
		t.Errorf("usage = %q", got)
	}
}

func TestEffortBareShowsCurrentAndOptions(t *testing.T) {
	c, take := settingsController(t)
	seedUserConfig(t, `default_model = "relay/m1"

[[providers]]
name = "relay"
kind = "openai"
base_url = "http://127.0.0.1:1/v1"
model = "m1"
api_key = "x"
reasoning_protocol = "openai"
effort = "high"
`)
	c.modelRef = "relay/m1"
	c.managementNotice("/effort")
	got := lastNotice(t, take())
	if !strings.HasPrefix(got, "effort for relay: high") || !strings.Contains(got, "options:") {
		t.Errorf("effort = %q", got)
	}
}

func TestRenameWithoutSession(t *testing.T) {
	c, take := settingsController(t)
	c.managementNotice("/rename")
	if got := lastNotice(t, take()); got != i18n.M.RenameUsage {
		t.Errorf("usage = %q", got)
	}
	c.managementNotice("/rename a title")
	if got := lastNotice(t, take()); got != i18n.M.RenameNoSession {
		t.Errorf("no session = %q", got)
	}
}

func TestRenameWritesTheSessionTitle(t *testing.T) {
	c, take := settingsController(t)
	path := filepath.Join(testenv.TempDir(t), "s.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c.sessionPath = path
	c.managementNotice("/rename Release notes")
	if got := lastNotice(t, take()); got != fmt.Sprintf(i18n.M.RenameDoneFmt, "Release notes") {
		t.Fatalf("notice = %q", got)
	}
	meta, ok, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !ok || meta.CustomTitle != "Release notes" {
		t.Fatalf("meta = %+v ok=%v err=%v", meta, ok, err)
	}
}

func TestSandboxReadsTheControllersWorkspaceNotTheProcessCwd(t *testing.T) {
	c, take := settingsController(t)
	if err := os.WriteFile(filepath.Join(c.WorkspaceRoot(), "reasonix.toml"), []byte("[sandbox]\nnetwork = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(testenv.TempDir(t))
	c.managementNotice("/sandbox")
	if got := lastNotice(t, take()); !strings.Contains(got, "network     false") {
		t.Fatalf("status did not come from the controller's workspace:\n%s", got)
	}
}

func TestLanguageNeverWritesIntoAProjectFile(t *testing.T) {
	c, take := settingsController(t)
	project := filepath.Join(c.WorkspaceRoot(), "reasonix.toml")
	const body = "# shared\n[agent]\nmax_steps = 7\n"
	if err := os.WriteFile(project, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(c.WorkspaceRoot())
	c.managementNotice("/language zh")
	take()
	if raw, _ := os.ReadFile(project); string(raw) != body {
		t.Fatalf("project file was rewritten:\n%s", raw)
	}
	if raw, _ := os.ReadFile(config.UserConfigPath()); !strings.Contains(string(raw), `"zh"`) {
		t.Fatalf("user config missing the language:\n%s", raw)
	}
}

func TestLanguageWarnsWhenTheProjectConfigOverridesIt(t *testing.T) {
	c, _ := settingsController(t)
	if err := os.WriteFile(filepath.Join(c.WorkspaceRoot(), "reasonix.toml"), []byte("language = \"en\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []event.Event
	c.sink = event.FuncSink(func(e event.Event) { got = append(got, e) })
	c.managementNotice("/language zh")
	for _, e := range got {
		if e.Code == event.NoticeCodeLanguageOverridden && e.Level == event.LevelWarn && e.Detail == "en" {
			return
		}
	}
	t.Fatalf("no typed override warning among %d events", len(got))
}
