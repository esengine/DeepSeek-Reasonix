package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

type sample struct {
	A bool   `toml:"a"`
	B string `toml:"b"`
}

func TestAnEntryWithoutAClassOrTimingFailsRegistration(t *testing.T) {
	root := reflect.TypeFor[sample]()
	for name, rows := range map[string][]Entry{
		"no class":  {{ID: "a", Effect: Immediate}, {ID: "b", Class: Preference, Effect: Immediate}},
		"no timing": {{ID: "a", Class: Preference}, {ID: "b", Class: Preference, Effect: Immediate}},
	} {
		if err := register(rows, root); err == nil {
			t.Errorf("%s: registered", name)
		}
	}
}

func TestAKeyNoRowDeclaresFailsRegistration(t *testing.T) {
	err := register([]Entry{{ID: "a", Class: Preference, Effect: Immediate}}, reflect.TypeFor[sample]())
	if CodeOf(err) != codeUndeclared || !strings.Contains(err.Error(), "b") {
		t.Fatalf("err = %v, want the undeclared key named", err)
	}
}

func TestARowForNoKeyFailsRegistration(t *testing.T) {
	rows := []Entry{{ID: "a", Class: Preference, Effect: Immediate}, {ID: "b", Class: Preference, Effect: Immediate}, {ID: "c", Class: Preference, Effect: Immediate}}
	if err := register(rows, reflect.TypeFor[sample]()); err == nil {
		t.Fatal("a declared key the config does not have was accepted")
	}
}

func TestTheDeclarationsCoverTheUserConfigExactly(t *testing.T) {
	if err := register(table, reflect.TypeFor[config.Config]()); err != nil {
		t.Fatal(err)
	}
	if len(All()) != len(fields) {
		t.Fatalf("declared %d, config has %d", len(All()), len(fields))
	}
}

func TestBoundaryKeysAreGuarded(t *testing.T) {
	for _, e := range All() {
		for _, prefix := range []string{"permissions.", "sandbox.", "network.", "environment."} {
			if strings.HasPrefix(e.ID, prefix) && e.Class != Guarded {
				t.Errorf("%s is under %s and must be Guarded", e.ID, prefix)
			}
		}
	}
	for _, id := range []string{
		"desktop.default_tool_approval_mode", "agent.plan_approval", "agent.subagent_claims",
		"agent.max_steps", "agent.max_subagent_depth", "agent.max_subagent_concurrency", "agent.max_parallel_writers",
		"agent.guardian_model", "agent.system_prompt", "agent.screen_external_content",
		"tools.shell.path", "tools.enabled", "statusline.command", "skills.paths", "providers", "plugins",
		"peers.release_projects", "serve.token", "serve.auth_mode", "secrets.protect_sensitive_files", "storage", "attachments.max_stream_bytes",
	} {
		if e, ok := Lookup(id); !ok || e.Class != Guarded {
			t.Errorf("%s = %+v, want Guarded", id, e)
		}
	}
}

func TestDispatchIsElevatedAndItsProjectListIsNot(t *testing.T) {
	for id, want := range map[string]Class{"peers.enabled": Elevated, "peers.dispatch": Elevated, "peers.release_projects": Guarded} {
		if e, _ := Lookup(id); e.Class != want {
			t.Errorf("%s class = %v, want %v", id, e.Class, want)
		}
	}
}

func TestASecretIsNeverRead(t *testing.T) {
	cfg := &config.Config{}
	cfg.Serve.Token = "hunter2"
	got, err := Read(cfg, "serve.token")
	if CodeOf(err) != CodeSecret || got != nil {
		t.Fatalf("got %v, %v; want the secret refusal and no value", got, err)
	}
}

func TestAGuardedKeyIsRefusedAndLeftAlone(t *testing.T) {
	cfg := &config.Config{}
	cfg.Permissions.Mode = "ask"
	_, _, err := Write(cfg, "permissions.mode", json.RawMessage(`"yolo"`))
	if CodeOf(err) != CodeGuarded || cfg.Permissions.Mode != "ask" {
		t.Fatalf("err = %v, mode = %q; want refused and unchanged", err, cfg.Permissions.Mode)
	}
}

func TestWriteReturnsWhatWasThereAndWhatIsNow(t *testing.T) {
	cfg := &config.Config{}
	cfg.UI.ShowReceipt = true
	from, to, err := Write(cfg, "ui.show_receipt", json.RawMessage(`false`))
	if err != nil || from != true || to != false || cfg.UI.ShowReceipt {
		t.Fatalf("from=%v to=%v err=%v show=%v", from, to, err, cfg.UI.ShowReceipt)
	}
	from, to, err = Write(cfg, "agent.max_queued_inputs", json.RawMessage(`7`))
	if err != nil || from != 0 || to != 7 || cfg.Agent.MaxQueuedInputs != 7 {
		t.Fatalf("from=%v to=%v err=%v", from, to, err)
	}
}

func TestAValueOfTheWrongTypeIsRefusedAndLeftAlone(t *testing.T) {
	cfg := &config.Config{}
	for _, raw := range []string{`"yes"`, `null`, `1.5`, `[]`} {
		before := *cfg
		_, _, err := Write(cfg, "ui.show_receipt", json.RawMessage(raw))
		if CodeOf(err) != CodeValue || !reflect.DeepEqual(before.UI, cfg.UI) {
			t.Errorf("%s: err = %v", raw, err)
		}
	}
}

func TestAListOfStringsAndNothingStructuredIsWritable(t *testing.T) {
	cfg := &config.Config{}
	if _, to, err := Write(cfg, "skills.disabled_skills", json.RawMessage(`["a","b"]`)); err != nil || !reflect.DeepEqual(to, []string{"a", "b"}) {
		t.Fatalf("to=%v err=%v", to, err)
	}
	if _, _, err := Write(cfg, "agent.role_efforts", json.RawMessage(`{"x":"high"}`)); CodeOf(err) != CodeShape {
		t.Fatalf("a table was written: %v", err)
	}
}

func TestAnUnknownKeyIsRefusedByCode(t *testing.T) {
	if _, _, err := Write(&config.Config{}, "ui.nope", json.RawMessage(`1`)); CodeOf(err) != CodeUnknown {
		t.Fatalf("err = %v", err)
	}
}

func TestShapesAreReadFromTheConfigType(t *testing.T) {
	for id, want := range map[string]Shape{
		"ui.show_receipt": ShapeBool, "agent.max_queued_inputs": ShapeInt, "agent.temperature": ShapeFloat,
		"ui.theme": ShapeString, "skills.disabled_skills": ShapeStringList, "agent.role_efforts": ShapeStructured,
	} {
		if got := ShapeOf(id); got != want {
			t.Errorf("%s shape = %v, want %v", id, got, want)
		}
	}
}

func TestAnOptionalKeyIsWrittenThroughItsPointer(t *testing.T) {
	cfg := &config.Config{}
	from, to, err := Write(cfg, "tools.browser_tools", json.RawMessage(`true`))
	if err != nil || from != nil || to != true || cfg.Tools.BrowserTools == nil || !*cfg.Tools.BrowserTools {
		t.Fatalf("from=%v to=%v err=%v", from, to, err)
	}
	if got, _ := Read(cfg, "tools.browser_tools"); got != true {
		t.Fatalf("read = %v", got)
	}
	if ShapeOf("tools.browser_tools") != ShapeBool {
		t.Fatal("an optional bool is not a bool")
	}
}

func TestNoStructuredKeyIsReadOrWritten(t *testing.T) {
	cfg := &config.Config{}
	seen := 0
	for _, e := range All() {
		if ShapeOf(e.ID) != ShapeStructured {
			continue
		}
		seen++
		if _, err := Read(cfg, e.ID); CodeOf(err) != CodeShape && CodeOf(err) != CodeSecret {
			t.Errorf("%s was read: %v", e.ID, err)
		}
	}
	if seen == 0 {
		t.Fatal("the config has no structured key left to hold this to")
	}
}

func TestKeysThatRelaxAProtectionAreElevated(t *testing.T) {
	for _, id := range []string{"progress_watch.pause", "progress_watch.rounds", "progress_watch.token_multiple", "progress_watch.perseveration_retries", "agent.worktree_isolation", "agent.write_lease"} {
		if e, _ := Lookup(id); e.Class != Elevated {
			t.Errorf("%s class = %v, want Elevated", id, e.Class)
		}
	}
}

func TestEveryWritableValueHasAValidator(t *testing.T) {
	for _, e := range All() {
		if e.Class == Guarded {
			continue
		}
		switch ShapeOf(e.ID) {
		case ShapeString, ShapeInt, ShapeFloat, ShapeStringList:
			if e.Via == nil && !e.Bounded && !e.Free {
				t.Errorf("%s accepts a value with nothing to say what a legal one is", e.ID)
			}
		}
	}
}

func TestAnOutOfRangeNumberIsRefusedAndLeftAlone(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agent.Temperature = 0.2
	for _, raw := range []string{`7`, `-1`} {
		if _, _, err := Write(cfg, "agent.temperature", json.RawMessage(raw)); CodeOf(err) != CodeRange || cfg.Agent.Temperature != 0.2 {
			t.Errorf("%s: err = %v, temperature = %v", raw, err, cfg.Agent.Temperature)
		}
	}
}

func TestTheConfigsOwnSetterJudgesTheValue(t *testing.T) {
	cfg := config.Default()
	if _, _, err := Write(cfg, "agent.compact_ratio", json.RawMessage(`5`)); CodeOf(err) != CodeValue {
		t.Fatalf("a ratio the setter refuses was written: %v", err)
	}
	if _, to, err := Write(cfg, "agent.compact_ratio", json.RawMessage(`0.8`)); err != nil || to != 0.8 {
		t.Fatalf("to=%v err=%v", to, err)
	}
}

func TestARemedyFollowsTheKeysClass(t *testing.T) {
	if r := RemedyFor("peers.dispatch", true); r == nil || r.Op != "propose" || string(r.Value) != "true" {
		t.Fatalf("elevated remedy = %+v", r)
	}
	if r := RemedyFor("permissions.mode", "yolo"); r == nil || r.Op != "open" || len(r.Value) != 0 {
		t.Fatalf("guarded remedy = %+v, want open without a value", r)
	}
	if RemedyFor("no.such.key", 1) != nil {
		t.Fatal("a remedy for a key nobody declared")
	}
}

func TestClassAndEffectHaveWireNames(t *testing.T) {
	for _, e := range All() {
		if e.Class.String() == "" || e.Effect.String() == "" {
			t.Fatalf("%s has no wire name", e.ID)
		}
	}
}

func TestAWebBackendChainIsJudgedByTheConfigsOwnNormaliser(t *testing.T) {
	cfg := &config.Config{}
	if _, to, err := Write(cfg, "tools.web_fetch_backends", json.RawMessage(`["local","deepseek_api"]`)); err != nil || !reflect.DeepEqual(to, []string{"local", "deepseek_api"}) {
		t.Fatalf("to=%v err=%v", to, err)
	}
	if _, _, err := Write(cfg, "tools.web_search_backends", json.RawMessage(`["no-such-backend"]`)); CodeOf(err) != CodeValue || len(cfg.Tools.WebSearchBackends) != 0 {
		t.Fatalf("a backend nothing can build was written: %v", err)
	}
}

func TestEmbeddedConfigStructsAreDeclaredToo(t *testing.T) {
	for _, id := range []string{"tools.searxng_url", "tools.web_search_paid_allowed", "tools.web_fetch_paid_allowed"} {
		if e, ok := Lookup(id); !ok || e.Class != Guarded {
			t.Errorf("%s = %+v, want a declared Guarded key", id, e)
		}
	}
	if e, _ := Lookup("tools.web_search_backends"); e.Class != Elevated {
		t.Errorf("a chain that can add a paid backend must be Elevated, got %v", e.Class)
	}
}

// The desktop's table of where a setting is drawn names only ids that exist, so a
// setting renamed or removed here cannot leave a button that opens the wrong row.
func TestTheDesktopScreenTableNamesOnlyDeclaredSettings(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "desktop", "frontend-next", "src", "ui", "settingScreens.ts"))
	if err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`(?m)^\s+"([a-z_][a-z_.0-9]*)":\s+"[a-z-]+:[a-z-]+",`).FindAllStringSubmatch(string(src), -1)
	if len(ids) < 30 {
		t.Fatalf("read %d ids from the screen table; this guard is watching nothing", len(ids))
	}
	for _, m := range ids {
		if _, ok := Lookup(m[1]); !ok {
			t.Errorf("settingScreens.ts names %q, which no declaration has", m[1])
		}
	}
}
